package types

import (
	"context"
	"reflect"
	"strings"
	"sync"
)

// ICrossNodeForwarder 由集群包实现，用于跨节点转发 WebSocket 通知和订阅变化。
type ICrossNodeForwarder interface {
	// ForwardNotice 向订阅指定路由的其他节点转发消息。
	ForwardNotice(ctx context.Context, routePath string, hash uint64, message interface{})

	// OnSubscriptionChange 通知转发器本地订阅已增加或移除。
	OnSubscriptionChange(routePath string, hash uint64, active bool)

	// DrainAndStop 广播当前订阅已移除，并停止接收新的转发任务。
	DrainAndStop(ctx context.Context)
}

var (
	globalCrossNodeForwarder   ICrossNodeForwarder
	serviceCrossNodeForwarders = make(map[string]ICrossNodeForwarder)
	globalCrossNodeForwarderMu sync.RWMutex
)

// SetCrossNodeForwarder 设置旧版全局兼容转发器。
// Deprecated: 新代码应使用 SetCrossNodeForwarderForService。
func SetCrossNodeForwarder(f ICrossNodeForwarder) {
	if isNilCrossNodeForwarder(f) {
		f = nil
	}
	globalCrossNodeForwarderMu.Lock()
	globalCrossNodeForwarder = f
	globalCrossNodeForwarderMu.Unlock()
}

// GetCrossNodeForwarder 返回旧版全局兼容转发器。
// Deprecated: 新代码应使用 GetCrossNodeForwarderForService。
func GetCrossNodeForwarder() ICrossNodeForwarder {
	globalCrossNodeForwarderMu.RLock()
	f := globalCrossNodeForwarder
	globalCrossNodeForwarderMu.RUnlock()
	return f
}

// SetCrossNodeForwarderForService 为指定服务登记跨节点转发器。
func SetCrossNodeForwarderForService(serviceName string, f ICrossNodeForwarder) {
	name := normalizeCrossNodeServiceName(serviceName)
	if isNilCrossNodeForwarder(f) {
		f = nil
	}
	if name == "" {
		SetCrossNodeForwarder(f)
		return
	}

	globalCrossNodeForwarderMu.Lock()
	if f == nil {
		delete(serviceCrossNodeForwarders, name)
	} else {
		serviceCrossNodeForwarders[name] = f
	}
	globalCrossNodeForwarderMu.Unlock()
}

// GetCrossNodeForwarderForService 返回服务转发器。服务级运行时不回退到旧版
// 全局转发器，避免一个服务的兼容配置被其他服务静默继承。
func GetCrossNodeForwarderForService(serviceName string) ICrossNodeForwarder {
	name := normalizeCrossNodeServiceName(serviceName)
	globalCrossNodeForwarderMu.RLock()
	f := serviceCrossNodeForwarders[name]
	globalCrossNodeForwarderMu.RUnlock()
	return f
}

// ClearCrossNodeForwarderForService 仅在当前值仍为 expected 时清除服务转发器。
func ClearCrossNodeForwarderForService(serviceName string, expected ICrossNodeForwarder) bool {
	name := normalizeCrossNodeServiceName(serviceName)
	if name == "" {
		globalCrossNodeForwarderMu.Lock()
		defer globalCrossNodeForwarderMu.Unlock()
		if !sameCrossNodeForwarder(globalCrossNodeForwarder, expected) {
			return false
		}
		globalCrossNodeForwarder = nil
		return true
	}

	globalCrossNodeForwarderMu.Lock()
	defer globalCrossNodeForwarderMu.Unlock()
	current, ok := serviceCrossNodeForwarders[name]
	if !ok || !sameCrossNodeForwarder(current, expected) {
		return false
	}
	delete(serviceCrossNodeForwarders, name)
	return true
}

func normalizeCrossNodeServiceName(serviceName string) string {
	return strings.ToLower(strings.TrimSpace(serviceName))
}

func sameCrossNodeForwarder(left, right ICrossNodeForwarder) bool {
	leftNil := isNilCrossNodeForwarder(left)
	rightNil := isNilCrossNodeForwarder(right)
	if leftNil || rightNil {
		return leftNil && rightNil
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	return leftValue.Type() == rightValue.Type() &&
		leftValue.Comparable() &&
		leftValue.Equal(rightValue)
}

func isNilCrossNodeForwarder(forwarder ICrossNodeForwarder) bool {
	if forwarder == nil {
		return true
	}
	value := reflect.ValueOf(forwarder)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
