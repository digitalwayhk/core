package types

import (
	"sync/atomic"
	"testing"
	"time"
)

type fakeTakeRouteCacheRuntime struct {
	fakeRouteCacheRuntime
	takes atomic.Int32
}

func (f *fakeTakeRouteCacheRuntime) TakeBestEffort(_ string, _ interface{}, _ time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	f.takes.Add(1)
	return loader()
}

type cacheCaptureResponse struct {
	shardTestResponse
	data interface{}
	err  error
}

type cacheCaptureRequest struct{ shardTestRequest }

func (*cacheCaptureRequest) NewResponse(data interface{}, err error) IResponse {
	return &cacheCaptureResponse{data: data, err: err}
}

type cacheLoaderRouter struct{ calls *atomic.Int32 }

func (*cacheLoaderRouter) Parse(IRequest) error      { return nil }
func (*cacheLoaderRouter) Validation(IRequest) error { return nil }
func (r *cacheLoaderRouter) Do(IRequest) (interface{}, error) {
	r.calls.Add(1)
	return "loaded", nil
}
func (*cacheLoaderRouter) RouterInfo() *RouterInfo { return nil }
func (*cacheLoaderRouter) GetCacheKey() string     { return "same" }

func TestRouterInfoExecDoUsesOptionalTakeRuntime(t *testing.T) {
	runtime := &fakeTakeRouteCacheRuntime{}
	info := &RouterInfo{Path: "/api/items", ServiceName: "test"}
	info.SetCacheManager("test", runtime)
	info.UseCache(time.Second)
	var calls atomic.Int32
	router := &cacheLoaderRouter{calls: &calls}

	response := info.ExecDo(router, &cacheCaptureRequest{})
	captured, ok := response.(*cacheCaptureResponse)
	if !ok {
		t.Fatalf("response type = %T, want *cacheCaptureResponse", response)
	}
	if captured.err != nil || captured.data != "loaded" {
		t.Fatalf("response = data:%#v err:%v", captured.data, captured.err)
	}
	if runtime.takes.Load() != 1 {
		t.Fatalf("TakeBestEffort calls = %d, want 1", runtime.takes.Load())
	}
	if calls.Load() != 1 {
		t.Fatalf("Do calls = %d, want 1", calls.Load())
	}
}
