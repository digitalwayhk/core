// Package router 验证 ServiceContext 实例级资源的注册隔离、逆序关闭、错误汇总和幂等边界。
package router

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type resourceFunc func(context.Context) error

func (f resourceFunc) Close(ctx context.Context) error { return f(ctx) }

func TestResourceManagerClosesInReverseOrderAndJoinsErrors(t *testing.T) {
	manager := newResourceManager()
	var (
		mu    sync.Mutex
		order []string
	)
	errA := errors.New("close-a")
	require.NoError(t, manager.Use("a", resourceFunc(func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "a")
		return errA
	})))
	require.NoError(t, manager.Use("b", resourceFunc(func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "b")
		return nil
	})))

	err := manager.Close(context.Background())
	require.ErrorIs(t, err, errA)
	require.Equal(t, []string{"b", "a"}, order)
	require.ErrorIs(t, manager.Use("late", resourceFunc(func(context.Context) error { return nil })), ErrResourceManagerClosed)
	require.ErrorIs(t, manager.Close(context.Background()), errA)
}

func TestResourceManagerRejectsInvalidAndDuplicateRegistration(t *testing.T) {
	manager := newResourceManager()
	require.ErrorIs(t, manager.Use("", resourceFunc(func(context.Context) error { return nil })), ErrInvalidManagedResource)
	require.ErrorIs(t, manager.Use("nil", nil), ErrInvalidManagedResource)
	require.NoError(t, manager.Use("orders", resourceFunc(func(context.Context) error { return nil })))
	require.ErrorIs(t, manager.Use("orders", resourceFunc(func(context.Context) error { return nil })), ErrResourceAlreadyRegistered)
}

func TestResourceManagerContinuesClosingAfterContextCancellation(t *testing.T) {
	manager := newResourceManager()
	var closed []string
	require.NoError(t, manager.Use("first", resourceFunc(func(ctx context.Context) error {
		closed = append(closed, "first")
		return ctx.Err()
	})))
	require.NoError(t, manager.Use("second", resourceFunc(func(ctx context.Context) error {
		closed = append(closed, "second")
		return ctx.Err()
	})))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := manager.Close(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"second", "first"}, closed)
}

func TestServiceContextResourcesAreInstanceScoped(t *testing.T) {
	first := &ServiceContext{resources: newResourceManager()}
	second := &ServiceContext{resources: newResourceManager()}
	require.NoError(t, first.UseResource("orders", resourceFunc(func(context.Context) error { return nil })))
	require.NoError(t, second.UseResource("orders", resourceFunc(func(context.Context) error { return nil })))
}
