package event

import (
	"context"
	"testing"
)

func BenchmarkStreamPublishTenSubscribers(b *testing.B) {
	stream := NewStream()
	envelope := NewEnvelope("benchmark", "order.updated", []byte(`{"id":1}`))
	for i := 0; i < 10; i++ {
		_, err := stream.Subscribe(envelope.Type, func(*Envelope) {})
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := stream.Publish(context.Background(), envelope); err != nil {
				b.Fatal(err)
			}
		}
	})
}
