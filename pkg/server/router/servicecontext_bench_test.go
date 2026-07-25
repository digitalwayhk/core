package router

import "testing"

func BenchmarkServiceContextRegistryLookup(b *testing.B) {
	registry := newServiceContextRegistry()
	want := &ServiceContext{}
	registry.contexts["orders"] = want

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if got := registry.get("orders"); got != want {
				b.Fatal("registry 返回错误实例")
			}
		}
	})
}
