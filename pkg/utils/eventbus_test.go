package utils

import (
	"sync"
	"testing"
	"time"
)

func TestPublisher_PublishSubscribe(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 8)
	defer p.Close()

	sub := p.Subscribe()
	go p.Publish("hello")

	select {
	case got := <-sub:
		if got != "hello" {
			t.Fatalf("got %v want hello", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestPublisher_TopicFilter(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 8)
	defer p.Close()

	stringsOnly := p.SubscribeTopic(func(v interface{}) bool {
		_, ok := v.(string)
		return ok
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.Publish(42)        // filtered out
		p.Publish("welcome") // delivered
	}()

	select {
	case got := <-stringsOnly:
		if got != "welcome" {
			t.Fatalf("got %v want welcome", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for filtered message")
	}
	wg.Wait()
}

func TestPublisher_Evict(t *testing.T) {
	p := NewPublisher(50*time.Millisecond, 4)
	defer p.Close()

	sub := p.Subscribe()
	p.Evict(sub)

	// After eviction, the channel must be closed.
	if _, ok := <-sub; ok {
		t.Fatal("expected channel to be closed after Evict")
	}
}

func TestPublisher_Close(t *testing.T) {
	p := NewPublisher(50*time.Millisecond, 4)
	a := p.Subscribe()
	b := p.Subscribe()
	p.Close()

	for i, ch := range []chan interface{}{a, b} {
		if _, ok := <-ch; ok {
			t.Fatalf("subscriber %d: channel should be closed after Close", i)
		}
	}
}

func TestPublisher_PublishTimeout(t *testing.T) {
	// Buffer 1 + slow subscriber: second publish must not block forever; it
	// should give up after the configured timeout. We assert the call returns.
	p := NewPublisher(20*time.Millisecond, 1)
	defer p.Close()

	_ = p.Subscribe() // never drained

	done := make(chan struct{})
	go func() {
		p.Publish("a") // fits in buffer
		p.Publish("b") // should time out, not block
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish did not honor timeout")
	}
}
