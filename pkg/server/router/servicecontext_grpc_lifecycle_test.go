package router_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type grpcLifecycleRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *grpcLifecycleRecorder) add(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *grpcLifecycleRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

type readyGRPCServer struct {
	ready      chan struct{}
	done       chan struct{}
	readyOnce  sync.Once
	doneOnce   sync.Once
	stopOnce   sync.Once
	errMu      sync.RWMutex
	err        error
	recorder   *grpcLifecycleRecorder
	startCalls atomic.Int32
	stopCalls  atomic.Int32
}

func newReadyGRPCServer(recorder *grpcLifecycleRecorder) *readyGRPCServer {
	return &readyGRPCServer{ready: make(chan struct{}), done: make(chan struct{}), recorder: recorder}
}

func (s *readyGRPCServer) Start() {
	s.startCalls.Add(1)
	<-s.done
}

func (s *readyGRPCServer) Stop() {
	_ = s.StopContext(context.Background())
}

func (s *readyGRPCServer) Ready() <-chan struct{} { return s.ready }
func (s *readyGRPCServer) Done() <-chan struct{}  { return s.done }

func (s *readyGRPCServer) BeginShutdown() {
	if s.recorder != nil {
		s.recorder.add("not-serving")
	}
}

func (s *readyGRPCServer) StopContext(context.Context) error {
	s.stopOnce.Do(func() {
		s.stopCalls.Add(1)
		if s.recorder != nil {
			s.recorder.add("grpc-stop")
		}
		s.doneOnce.Do(func() { close(s.done) })
	})
	return nil
}

func (s *readyGRPCServer) Err() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.err
}

func (s *readyGRPCServer) MarkReady() { s.readyOnce.Do(func() { close(s.ready) }) }

func (s *readyGRPCServer) Fail(err error) {
	s.errMu.Lock()
	s.err = err
	s.errMu.Unlock()
	s.doneOnce.Do(func() { close(s.done) })
}

type orderedLifecycleProvider struct {
	lifecycleProvider
	recorder *grpcLifecycleRecorder
	lastNode atomic.Pointer[cluster.NodeInfo]
}

func (p *orderedLifecycleProvider) Register(ctx context.Context, node *cluster.NodeInfo) error {
	copyNode := *node
	p.lastNode.Store(&copyNode)
	if p.recorder != nil {
		p.recorder.add("register")
	}
	return p.lifecycleProvider.Register(ctx, node)
}

func (p *orderedLifecycleProvider) Deregister(ctx context.Context, nodeID string) error {
	if p.recorder != nil {
		p.recorder.add("deregister")
	}
	return p.lifecycleProvider.Deregister(ctx, nodeID)
}

func TestNodeRegistersOnlyAfterGRPCIsServing(t *testing.T) {
	provider := &orderedLifecycleProvider{}
	rpcServer := newReadyGRPCServer(nil)
	sc := router.NewServiceContext(&fakeService{name: "sctest-grpc-ready-register"})
	sc.Config.Transport.GRPC.Port = 19090
	sc.ClusterProvider = provider
	sc.SetGRPCServer(rpcServer)

	started := make(chan struct{})
	go func() {
		sc.SetRunState(true)
		close(started)
	}()

	assert.Never(t, func() bool { return provider.registerCount.Load() != 0 }, 50*time.Millisecond, 5*time.Millisecond)
	rpcServer.MarkReady()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("等待 gRPC Ready 后注册超时")
	}
	t.Cleanup(func() { sc.SetRunState(false) })

	require.NotNil(t, provider.lastNode.Load())
	assert.Equal(t, 19090, provider.lastNode.Load().GRPCPort)
}

func TestServiceContextGRPCShutdownOrder(t *testing.T) {
	recorder := &grpcLifecycleRecorder{}
	provider := &orderedLifecycleProvider{recorder: recorder}
	rpcServer := newReadyGRPCServer(recorder)
	sc := router.NewServiceContext(&fakeService{name: "sctest-grpc-shutdown-order"})
	sc.ClusterProvider = provider
	sc.SetGRPCServer(rpcServer)
	rpcServer.MarkReady()

	sc.SetRunState(true)
	sc.SetRunState(false)

	assert.Equal(t, []string{"register", "not-serving", "deregister", "grpc-stop"}, recorder.snapshot())
}

func TestServiceContextGRPCFailureRevokesDiscovery(t *testing.T) {
	provider := &orderedLifecycleProvider{}
	rpcServer := newReadyGRPCServer(nil)
	sc := router.NewServiceContext(&fakeService{name: "sctest-grpc-runtime-failure"})
	sc.ClusterProvider = provider
	sc.SetGRPCServer(rpcServer)
	rpcServer.MarkReady()
	sc.SetRunState(true)

	wantErr := errors.New("grpc serve failed")
	rpcServer.Fail(wantErr)

	require.Eventually(t, func() bool { return provider.deregisterCount.Load() == 1 }, time.Second, 5*time.Millisecond)
	assert.ErrorIs(t, sc.RuntimeError(), wantErr)
	assert.False(t, sc.IsRun())
}
