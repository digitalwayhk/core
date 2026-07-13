package types

import "testing"

func BenchmarkWebSocketNotificationQueueSubmit(b *testing.B) {
	system := &WebSocketNotificationSystem{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if system.Submit(nil) {
			b.Fatal("废弃兼容壳不应接受任务")
		}
	}
}
