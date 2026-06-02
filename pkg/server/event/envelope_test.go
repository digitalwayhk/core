package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvelope_FieldsPresent(t *testing.T) {
	env := event.NewEnvelope("com.example.service", "com.example.user.created", []byte(`{"id":1}`))
	env.TraceID = "trace-abc"
	env.IdempotencyKey = "idem-xyz"
	env.ShardKey = "user-1"

	assert.NotEmpty(t, env.ID)
	assert.Equal(t, "1.0", env.SpecVersion)
	assert.Equal(t, "com.example.service", env.Source)
	assert.Equal(t, "com.example.user.created", env.Type)
	assert.Equal(t, "application/json", env.DataContentType)
	assert.WithinDuration(t, time.Now(), env.Time, 5*time.Second)
	assert.Equal(t, []byte(`{"id":1}`), env.Data)
	assert.Equal(t, "trace-abc", env.TraceID)
	assert.Equal(t, "idem-xyz", env.IdempotencyKey)
	assert.Equal(t, "user-1", env.ShardKey)
}

func TestEnvelope_JSONRoundTrip(t *testing.T) {
	env := event.NewEnvelope("source", "test.event", []byte(`"hello"`))
	env.TraceID = "t1"
	env.IdempotencyKey = "i1"
	env.ShardKey = "s1"

	data, err := json.Marshal(env)
	require.NoError(t, err)

	var decoded event.Envelope
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, env.ID, decoded.ID)
	assert.Equal(t, env.Source, decoded.Source)
	assert.Equal(t, env.Type, decoded.Type)
	assert.Equal(t, env.SpecVersion, decoded.SpecVersion)
	assert.Equal(t, env.DataContentType, decoded.DataContentType)
	assert.Equal(t, env.TraceID, decoded.TraceID)
	assert.Equal(t, env.IdempotencyKey, decoded.IdempotencyKey)
	assert.Equal(t, env.ShardKey, decoded.ShardKey)
	assert.Equal(t, env.Data, decoded.Data)
}

func TestEnvelope_UniqueID(t *testing.T) {
	e1 := event.NewEnvelope("src", "type", nil)
	e2 := event.NewEnvelope("src", "type", nil)
	assert.NotEqual(t, e1.ID, e2.ID)
}
