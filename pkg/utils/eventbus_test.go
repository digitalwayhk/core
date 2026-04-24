package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPublisher_Subscribe_Publish(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 10)
	defer p.Close()

	ch := p.Subscribe()
	p.Publish("hello")

	select {
	case msg := <-ch:
		assert.Equal(t, "hello", msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestPublisher_SubscribeTopic_Filtered(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 10)
	defer p.Close()

	// Only receive strings starting with "ok"
	ch := p.SubscribeTopic(func(v interface{}) bool {
		s, ok := v.(string)
		return ok && len(s) >= 2 && s[:2] == "ok"
	})

	p.Publish("ok:message")
	p.Publish("skip:this")

	select {
	case msg := <-ch:
		assert.Equal(t, "ok:message", msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered message")
	}

	// "skip:this" should not arrive
	select {
	case msg := <-ch:
		t.Fatalf("unexpected message: %v", msg)
	case <-time.After(50 * time.Millisecond):
		// expected: nothing received
	}
}

func TestPublisher_Evict(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 10)

	ch := p.Subscribe()
	p.Evict(ch)

	// Channel should be closed
	_, open := <-ch
	assert.False(t, open)
}

func TestPublisher_MultipleSubscribers(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 10)
	defer p.Close()

	ch1 := p.Subscribe()
	ch2 := p.Subscribe()

	p.Publish(42)

	for _, ch := range []chan interface{}{ch1, ch2} {
		select {
		case msg := <-ch:
			assert.Equal(t, 42, msg)
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
}

func TestPublisher_Close_ClosesAllSubscribers(t *testing.T) {
	p := NewPublisher(100*time.Millisecond, 5)
	ch1 := p.Subscribe()
	ch2 := p.Subscribe()

	p.Close()

	// Both channels should be closed
	for _, ch := range []chan interface{}{ch1, ch2} {
		_, open := <-ch
		assert.False(t, open)
	}
}

func TestPublisher_Timeout(t *testing.T) {
	// Use a very short timeout; subscriber channel is full, message should be dropped
	p := NewPublisher(1*time.Millisecond, 0) // buffer=0: unbuffered channel
	defer p.Close()

	_ = p.Subscribe() // subscriber with no reader → publish will timeout quickly
	// Should not block indefinitely
	done := make(chan struct{})
	go func() {
		p.Publish("will-timeout")
		close(done)
	}()

	select {
	case <-done:
		// OK: publish completed (message dropped due to timeout)
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked for too long")
	}
}
