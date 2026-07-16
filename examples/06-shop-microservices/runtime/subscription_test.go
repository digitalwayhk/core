package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type subscriptionStub struct {
	failSubject string
	cancelled   []string
}

func (s *subscriptionStub) SubscribeExternalControl(_ context.Context, subject string) (func(), error) {
	if subject == s.failSubject {
		return nil, errors.New("subscribe failed")
	}
	return func() { s.cancelled = append(s.cancelled, subject) }, nil
}

func TestSubscribeExternalControlsCancelsPartialSubscriptionsOnFailure(t *testing.T) {
	stub := &subscriptionStub{failSubject: "second"}

	cancels, err := SubscribeExternalControls(context.Background(), stub, "first", "second", "third")

	require.Error(t, err)
	assert.Nil(t, cancels)
	assert.Equal(t, []string{"first"}, stub.cancelled)
}

func TestSubscribeExternalControlsReturnsEveryCancellation(t *testing.T) {
	stub := &subscriptionStub{}

	cancels, err := SubscribeExternalControls(context.Background(), stub, "first", "second")

	require.NoError(t, err)
	require.Len(t, cancels, 2)
	for _, cancel := range cancels {
		cancel()
	}
	assert.Equal(t, []string{"first", "second"}, stub.cancelled)
}
