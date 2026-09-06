package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCallReliableHandlerConvertsPanicToFailure(t *testing.T) {
	err := callReliableHandler(context.Background(), 0, func(*Message) error {
		panic("broken handler")
	}, &Message{})

	require.ErrorContains(t, err, "handler panic")
}

func TestCallReliableHandlerReturnsConfiguredTimeout(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	started := time.Now()
	err := callReliableHandler(context.Background(), 20*time.Millisecond, func(*Message) error {
		<-blocked
		return errors.New("late failure")
	}, &Message{})

	require.ErrorContains(t, err, "handler timeout")
	require.Less(t, time.Since(started), 200*time.Millisecond)
}
