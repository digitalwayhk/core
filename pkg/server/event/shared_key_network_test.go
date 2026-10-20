package event_test

import (
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// 只阻断本轮子进程到专属 Redis 的连接；控制记录仍走专属 Redis 的直接地址。
type sharedRedisProxy struct {
	listener    net.Listener
	target      string
	mu          sync.Mutex
	cut         bool
	connections map[net.Conn]bool
	wg          sync.WaitGroup
}

func newSharedRedisProxy(t *testing.T, target string) *sharedRedisProxy {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &sharedRedisProxy{listener: l, target: target, connections: map[net.Conn]bool{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			p.wg.Add(1)
			go p.forward(c)
		}
	}()
	t.Cleanup(func() { _ = l.Close(); p.disconnect(true); p.wg.Wait() })
	return p
}
func (p *sharedRedisProxy) forward(c net.Conn) {
	defer p.wg.Done()
	p.mu.Lock()
	if p.cut {
		p.mu.Unlock()
		_ = c.Close()
		return
	}
	p.connections[c] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.connections, c); p.mu.Unlock(); _ = c.Close() }()
	upstream, err := net.DialTimeout("tcp", p.target, time.Second)
	if err != nil {
		return
	}
	p.mu.Lock()
	if p.cut {
		p.mu.Unlock()
		_ = upstream.Close()
		return
	}
	p.connections[upstream] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.connections, upstream); p.mu.Unlock(); _ = upstream.Close() }()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, c); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(c, upstream)
	_ = c.Close()
	<-done
}
func (p *sharedRedisProxy) disconnect(cut bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cut = cut
	if cut {
		for c := range p.connections {
			_ = c.Close()
		}
	}
}

func TestSharedKeyReliableNetworkLossLateOwnerAndRejoin(t *testing.T) {
	s := newSharedProcessSuite(t)
	proxy := newSharedRedisProxy(t, os.Getenv("CORE_TEST_REDIS_ADDR"))
	a := s.startWithBroker(proxy.listener.Addr().String())
	s.publish("user:A", "A1", true, false)
	s.wait("A1", "entered")
	b := s.start()
	lockKeys, err := s.client.Keys(s.ctx, s.prefix+":ordered-owner:users:fills:key:*").Result()
	require.NoError(t, err)
	require.Len(t, lockKeys, 1)
	proxy.disconnect(true)
	// 只加速本轮一个失联执行的 TTL，用于明确制造 ownership 变更；不是生产租约时长证明。
	require.NoError(t, s.client.PExpire(s.ctx, lockKeys[0], 10*time.Millisecond).Err())
	require.Eventually(t, func() bool {
		for _, r := range s.records() {
			if r.Body == "A1" && r.Phase == "entered" && r.PID == b.Process.Pid {
				return true
			}
		}
		return false
	}, 4*time.Second, 10*time.Millisecond)
	replacement := s.client.Get(s.ctx, lockKeys[0]).Val()
	require.NotEmpty(t, replacement)
	s.publish("user:A", "A2", false, false)
	proxy.disconnect(false)
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":release:A1:"+strconv.Itoa(a.Process.Pid), "1", 0).Err())
	require.Eventually(t, func() bool {
		for _, r := range s.records() {
			if r.Body == "A1" && r.Phase == "completed" && r.PID == a.Process.Pid {
				return true
			}
		}
		return false
	}, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, replacement, s.client.Get(s.ctx, lockKeys[0]).Val(), "旧 owner 迟到不得释放新租约")
	require.Eventually(t, func() bool { return s.client.XPending(s.ctx, s.prefix+":fills", "users").Val().Count == 2 }, time.Second, 10*time.Millisecond, "旧 owner 完成不能 ACK A1 或跳过 A2")
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":release:A1:"+strconv.Itoa(b.Process.Pid), "1", 0).Err())
	s.wait("A2", "completed")
	s.publish("user:C", "C1", true, false)
	first := s.wait("C1", "entered")
	s.publish("user:D", "D1", false, false)
	other := s.wait("D1", "completed")
	require.NotEqual(t, first.PID, other.PID, "恢复连接的实例应能重新分担独立 key")
	s.release("C1")
	s.wait("C1", "completed")
}
