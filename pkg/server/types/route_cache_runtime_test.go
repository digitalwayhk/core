package types

import (
	"testing"
	"time"
)

type fakeRouteCacheRuntime struct {
	enabled int
	sets    int
	deletes int
	value   interface{}
}

func (f *fakeRouteCacheRuntime) EnableRoute(string, time.Duration) error {
	f.enabled++
	return nil
}
func (f *fakeRouteCacheRuntime) Get(string, interface{}) (interface{}, bool, error) {
	return f.value, f.value != nil, nil
}
func (f *fakeRouteCacheRuntime) Set(string, interface{}, interface{}, time.Duration) error {
	f.sets++
	return nil
}
func (f *fakeRouteCacheRuntime) Delete(string, interface{}) error {
	f.deletes++
	return nil
}
func (f *fakeRouteCacheRuntime) DeleteRoute(string) error {
	f.deletes++
	return nil
}

func TestRouterInfoUseCacheDelegatesToManager(t *testing.T) {
	runtime := &fakeRouteCacheRuntime{value: "cached"}
	info := &RouterInfo{Path: "/api/items", ServiceName: "test"}
	info.SetCacheManager("test", runtime)
	info.UseCache(time.Second)
	api := &plainPoolRouter{}

	cache := info.getCache(api)
	if cache == nil || cache.data != "cached" {
		t.Fatalf("getCache() = %#v, want cached", cache)
	}
	info.setCache(api, "fresh")
	info.FailureCache(api)

	if runtime.enabled != 1 || runtime.sets != 1 || runtime.deletes != 1 {
		t.Fatalf("delegate counts = enabled:%d sets:%d deletes:%d", runtime.enabled, runtime.sets, runtime.deletes)
	}
}

func TestRouterInfoUseCacheRejectsFrozenMetadataMutation(t *testing.T) {
	runtime := &fakeRouteCacheRuntime{}
	info := &RouterInfo{Path: "/api/items", ServiceName: "test"}
	info.SetCacheManager("test", runtime)
	info.Freeze("test")
	info.Path = "/api/changed"

	defer func() {
		if recover() == nil {
			t.Fatal("冻结元数据被修改后 UseCache 必须 fail closed")
		}
		if runtime.enabled != 0 {
			t.Fatalf("缓存运行时不应收到被篡改的路由: enabled=%d", runtime.enabled)
		}
	}()
	info.UseCache(time.Second)
}
