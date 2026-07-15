package authstate

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	zeroredis "github.com/zeromicro/go-zero/core/stores/redis"
)

type atomicEvalClient struct {
	mu           sync.Mutex
	available    bool
	states       map[string]State
	events       map[string]ApplyResult
	fingerprints map[string]string
	evalCalls    int
	applyCalls   int
}

type fixedEvalClient struct {
	reply interface{}
	err   error
}

func (f fixedEvalClient) EvalCtx(context.Context, string, []string, ...interface{}) (interface{}, error) {
	return f.reply, f.err
}

func newAtomicEvalClient() *atomicEvalClient {
	return &atomicEvalClient{available: true, states: make(map[string]State), events: make(map[string]ApplyResult), fingerprints: make(map[string]string)}
}

func (f *atomicEvalClient) EvalCtx(_ context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalCalls++
	if !f.available {
		return nil, errors.New("redis unavailable")
	}
	if strings.Contains(script, "authstate_apply_v1") {
		f.applyCalls++
		if result, ok := f.events[keys[0]]; ok {
			if f.fingerprints[keys[0]] != args[5].(string) {
				return []interface{}{int64(-1), int64(result.Generation), int64(0), result.State.EventOrder, result.State.UID, int64(0)}, nil
			}
			result.Applied = false
			return redisApplyReply(result), nil
		}
		state := f.states[keys[1]]
		order := args[2].(int64)
		if order > 0 && state.EventOrder > 0 && order <= state.EventOrder {
			result := ApplyResult{Applied: false, Generation: state.Generation, State: state}
			f.events[keys[0]] = result
			f.fingerprints[keys[0]] = args[5].(string)
			return redisApplyReply(result), nil
		}
		if args[1].(int64) == 1 {
			state.Generation++
		}
		if args[3].(int64) == 1 {
			state.Blocked = true
		}
		state.EventOrder = order
		state.UID = args[4].(string)
		f.states[keys[1]] = state
		result := ApplyResult{Applied: true, Generation: state.Generation, State: state}
		f.events[keys[0]] = result
		f.fingerprints[keys[0]] = args[5].(string)
		return redisApplyReply(result), nil
	}
	return nil, errors.New("unexpected script")
}

func redisApplyReply(result ApplyResult) []interface{} {
	applied := int64(0)
	if result.Applied {
		applied = 1
	}
	blocked := int64(0)
	if result.State.Blocked {
		blocked = 1
	}
	return []interface{}{applied, int64(result.Generation), blocked, result.State.EventOrder, result.State.UID, int64(0)}
}

func TestRedisApplyEventIsAtomicAndIdempotent(t *testing.T) {
	client := newAtomicEvalClient()
	store := NewRedisStore(client, "core:authrevocation")
	event := testCasdoorEvent("evt-1", "logout", 1, false)

	const workers = 64
	start := make(chan struct{})
	results := make(chan ApplyResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := store.Apply(context.Background(), event, time.Hour)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	applied := 0
	for result := range results {
		if result.Applied {
			applied++
		}
		require.Equal(t, uint64(1), result.Generation)
	}
	require.Equal(t, 1, applied)
	require.Equal(t, workers, client.applyCalls)
}

func TestRedisApplyUsesSameClusterHashTag(t *testing.T) {
	client := newAtomicEvalClient()
	store := NewRedisStore(client, "core:authrevocation")
	event := testCasdoorEvent("evt-1", "logout", 1, false)

	_, err := store.Apply(context.Background(), event, time.Hour)
	require.NoError(t, err)
	eventKey, stateKey := store.eventKey(event), store.stateKey(eventIdentityKey(event))
	require.Equal(t, redisHashTag(eventKey), redisHashTag(stateKey))
	require.NotEmpty(t, redisHashTag(eventKey))
}

func TestRedisCurrentRejectsNonStringUID(t *testing.T) {
	store := NewRedisStore(fixedEvalClient{reply: []interface{}{int64(1), int64(0), int64(2), struct{}{}}}, "test")

	_, err := store.Current(context.Background(), testIdentityKey())
	require.Error(t, err)
}

func TestRedisNilClientReturnsErrorInsteadOfPanicking(t *testing.T) {
	store := NewRedisStore(nil, "test")
	require.NotPanics(t, func() {
		_, err := store.Current(context.Background(), testIdentityKey())
		require.Error(t, err)
	})
}

func TestRedisConfirmActiveParsesSuccessAndGenerationConflict(t *testing.T) {
	key := testIdentityKey()
	success := NewRedisStore(fixedEvalClient{reply: []interface{}{int64(1), int64(7), int64(9), "user-1"}}, "test")
	state, err := success.ConfirmActive(context.Background(), key, 7)
	require.NoError(t, err)
	require.Equal(t, uint64(7), state.Generation)
	require.False(t, state.Blocked)
	require.Equal(t, int64(9), state.EventOrder)

	conflict := NewRedisStore(fixedEvalClient{reply: []interface{}{int64(0), int64(8)}}, "test")
	_, err = conflict.ConfirmActive(context.Background(), key, 7)
	require.ErrorIs(t, err, ErrGenerationChanged)
}

func TestRedisRejectsSameEventIDWithDifferentPayload(t *testing.T) {
	client := newAtomicEvalClient()
	store := NewRedisStore(client, "test")
	event := testCasdoorEvent("evt-1", "logout", 1, false)
	_, err := store.Apply(context.Background(), event, time.Hour)
	require.NoError(t, err)
	event.EventType = "delete-user"
	event.Blocked = true

	_, err = store.Apply(context.Background(), event, time.Hour)
	require.ErrorIs(t, err, ErrInvalidEvent)
}

func TestRedisIntegrationWhenExplicitlyEnabled(t *testing.T) {
	if os.Getenv("CORE_TEST_REDIS") != "1" {
		t.Skip("设置 CORE_TEST_REDIS=1 后运行 Redis 集成测试")
	}
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	require.NotEmpty(t, addr)
	client, err := zeroredis.NewRedis(zeroredis.RedisConf{Host: addr, Type: zeroredis.NodeType, NonBlock: false, PingTimeout: time.Second})
	require.NoError(t, err)
	store := NewRedisStore(client, "core:test:authstate:"+time.Now().Format("150405.000000000"))
	event := testCasdoorEvent("evt-real", "logout", 1, false)
	result, err := store.Apply(context.Background(), event, time.Minute)
	require.NoError(t, err)
	require.True(t, result.Applied)
	duplicate, err := store.Apply(context.Background(), event, time.Minute)
	require.NoError(t, err)
	require.False(t, duplicate.Applied)
	require.Equal(t, result.Generation, duplicate.Generation)
	current, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.Equal(t, result.Generation, current.Generation)

	require.NoError(t, store.MarkControlPublished(context.Background(), event))
	published, err := store.Apply(context.Background(), event, time.Minute)
	require.NoError(t, err)
	require.True(t, published.ControlPublished)

	confirmed, err := store.ConfirmActive(context.Background(), testIdentityKey(), current.Generation)
	require.NoError(t, err)
	require.False(t, confirmed.Blocked)
	_, err = store.ConfirmActive(context.Background(), testIdentityKey(), current.Generation+1)
	require.ErrorIs(t, err, ErrGenerationChanged)
}

func redisHashTag(key string) string {
	start := strings.IndexByte(key, '{')
	end := strings.IndexByte(key, '}')
	if start < 0 || end <= start+1 {
		return ""
	}
	return key[start+1 : end]
}
