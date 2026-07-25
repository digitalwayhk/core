// Package router 提供 ServiceContext 实例级资源的注册与有界关闭能力。
package router

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
)

var (
	// ErrResourceManagerClosed 表示当前 ServiceContext 已进入资源关闭阶段。
	ErrResourceManagerClosed = errors.New("服务资源管理器已关闭")
	// ErrResourceAlreadyRegistered 表示同一 ServiceContext 内资源名称重复。
	ErrResourceAlreadyRegistered = errors.New("服务资源名称已注册")
	// ErrInvalidManagedResource 表示资源名称为空或资源实例为 nil。
	ErrInvalidManagedResource = errors.New("服务资源无效")
)

// ManagedResource 定义由 ServiceContext 统一关闭的实例级资源。
type ManagedResource interface {
	Close(context.Context) error
}

type managedResourceEntry struct {
	name     string
	resource ManagedResource
}

type resourceManager struct {
	mu      sync.Mutex
	entries []managedResourceEntry
	names   map[string]struct{}
	closed  bool
	once    sync.Once
	err     error
}

func newResourceManager() *resourceManager {
	return &resourceManager{names: make(map[string]struct{})}
}

func (m *resourceManager) Use(name string, resource ManagedResource) error {
	name = strings.TrimSpace(name)
	if name == "" || isNilManagedResource(resource) {
		return ErrInvalidManagedResource
	}
	if m == nil {
		return ErrResourceManagerClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrResourceManagerClosed
	}
	if _, exists := m.names[name]; exists {
		return ErrResourceAlreadyRegistered
	}
	m.names[name] = struct{}{}
	m.entries = append(m.entries, managedResourceEntry{name: name, resource: resource})
	return nil
}

func (m *resourceManager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		m.mu.Lock()
		m.closed = true
		entries := append([]managedResourceEntry(nil), m.entries...)
		m.mu.Unlock()

		closeErrors := make([]error, 0, len(entries))
		for index := len(entries) - 1; index >= 0; index-- {
			if err := entries[index].resource.Close(ctx); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
		m.mu.Lock()
		m.err = errors.Join(closeErrors...)
		m.mu.Unlock()
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func isNilManagedResource(resource ManagedResource) bool {
	if resource == nil {
		return true
	}
	value := reflect.ValueOf(resource)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
