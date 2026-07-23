package types

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type crossNodeRegistryCapture struct {
	mu            sync.Mutex
	subscriptions int
	notices       int
}

func (c *crossNodeRegistryCapture) ForwardNotice(context.Context, string, uint64, interface{}) {
	c.mu.Lock()
	c.notices++
	c.mu.Unlock()
}

func (c *crossNodeRegistryCapture) OnSubscriptionChange(string, uint64, bool) {
	c.mu.Lock()
	c.subscriptions++
	c.mu.Unlock()
}

func (c *crossNodeRegistryCapture) DrainAndStop(context.Context) {}

func TestCrossNodeForwarderRegistryIsolatesServices(t *testing.T) {
	prefix := fmt.Sprintf("cross-node-%d", time.Now().UnixNano())
	serviceA := prefix + "-A"
	serviceB := prefix + "-B"
	forwarderA := &crossNodeRegistryCapture{}
	forwarderB := &crossNodeRegistryCapture{}
	SetCrossNodeForwarderForService(serviceA, forwarderA)
	SetCrossNodeForwarderForService(serviceB, forwarderB)
	t.Cleanup(func() {
		ClearCrossNodeForwarderForService(serviceA, forwarderA)
		ClearCrossNodeForwarderForService(serviceB, forwarderB)
	})

	if got := GetCrossNodeForwarderForService(serviceA); got != forwarderA {
		t.Fatalf("服务 A 获取了错误的转发器：%T", got)
	}
	if got := GetCrossNodeForwarderForService(serviceB); got != forwarderB {
		t.Fatalf("服务 B 获取了错误的转发器：%T", got)
	}
}

func TestClearCrossNodeForwarderDoesNotDeleteReplacement(t *testing.T) {
	serviceName := fmt.Sprintf("cross-node-replace-%d", time.Now().UnixNano())
	oldForwarder := &crossNodeRegistryCapture{}
	newForwarder := &crossNodeRegistryCapture{}
	SetCrossNodeForwarderForService(serviceName, oldForwarder)
	SetCrossNodeForwarderForService(serviceName, newForwarder)
	t.Cleanup(func() {
		ClearCrossNodeForwarderForService(serviceName, newForwarder)
	})

	if ClearCrossNodeForwarderForService(serviceName, oldForwarder) {
		t.Fatal("旧转发器清除了已经替换的新转发器")
	}
	if got := GetCrossNodeForwarderForService(serviceName); got != newForwarder {
		t.Fatalf("替换后的转发器丢失：%T", got)
	}
	if !ClearCrossNodeForwarderForService(serviceName, newForwarder) {
		t.Fatal("当前转发器未被清除")
	}
}

func TestCrossNodeForwarderRegistryDoesNotFallBackToLegacyGlobal(t *testing.T) {
	serviceName := fmt.Sprintf("cross-node-fallback-%d", time.Now().UnixNano())
	legacy := &crossNodeRegistryCapture{}
	SetCrossNodeForwarder(legacy)
	t.Cleanup(func() { SetCrossNodeForwarder(nil) })

	if got := GetCrossNodeForwarderForService(serviceName); got != nil {
		t.Fatalf("服务作用域查询不应回退到进程级兼容转发器：%T", got)
	}

	scoped := &crossNodeRegistryCapture{}
	SetCrossNodeForwarderForService(serviceName, scoped)
	t.Cleanup(func() {
		ClearCrossNodeForwarderForService(serviceName, scoped)
	})
	if got := GetCrossNodeForwarderForService(serviceName); got != scoped {
		t.Fatalf("服务作用域转发器未覆盖兼容转发器：%T", got)
	}
}

func TestCrossNodeForwarderRegistryConcurrentAccess(t *testing.T) {
	prefix := fmt.Sprintf("cross-node-concurrent-%d", time.Now().UnixNano())
	const workers = 64
	start := make(chan struct{})
	errs := make(chan string, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wait.Done()
			<-start
			serviceName := fmt.Sprintf("%s-%d", prefix, worker)
			forwarder := &crossNodeRegistryCapture{}
			SetCrossNodeForwarderForService(serviceName, forwarder)
			if got := GetCrossNodeForwarderForService(serviceName); got != forwarder {
				errs <- fmt.Sprintf("服务 %s 获取了错误的转发器", serviceName)
			}
			if !ClearCrossNodeForwarderForService(serviceName, forwarder) {
				errs <- fmt.Sprintf("服务 %s 未清除当前转发器", serviceName)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCrossNodeForwarderRegistryTreatsTypedNilAsUnset(t *testing.T) {
	serviceName := fmt.Sprintf("cross-node-typed-nil-%d", time.Now().UnixNano())
	var forwarder *crossNodeRegistryCapture
	SetCrossNodeForwarderForService(serviceName, forwarder)
	if got := GetCrossNodeForwarderForService(serviceName); got != nil {
		t.Fatalf("类型化 nil 被错误登记为转发器：%T", got)
	}
}
