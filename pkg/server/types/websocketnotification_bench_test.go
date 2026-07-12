package types

import "testing"

func BenchmarkWebSocketNotificationQueueSubmit(b *testing.B) {
	system := &WebSocketNotificationSystem{jobChan: make(chan *noticeJob, 1)}
	system.isStarted.Store(true)
	job := &noticeJob{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !system.Submit(job) {
			b.Fatal("提交任务失败")
		}
		<-system.jobChan
	}
}
