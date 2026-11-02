// Package contract 定义演示的稳定服务名和有界归属范围，不包含Broker内部键。
package contract

import (
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"time"
)

const (
	// ServiceName 是处理副本及Gateway共用的逻辑服务。
	ServiceName = "owned-demo"
	// Scope 是两个主题共享的演示归属范围。
	Scope = "example11-user-summary"
)

// Manifest 显式列出演示用户A/B；实际项目由应用提供稳定的有限归属目录。
func Manifest() ownership.Manifest {
	return (ownership.Manifest{Scope: Scope, Service: ServiceName, Group: ServiceName, Selector: "authenticated-user-v1", Keys: []string{"A", "B"}, Subjects: []string{"demo11.input.one", "demo11.input.two"}, Window: 8, MaxBatch: 8}).Normalize()
}

// DynamicManifest 显式启用通用追加目录，默认固定演示保持不变。
func DynamicManifest() ownership.Manifest {
	m := Manifest()
	m.Keys = nil
	m.DynamicKeys = &ownership.DynamicKeyPolicy{MaxKeys: 16, PageSize: 2, RefreshInterval: 100 * time.Millisecond, UnregisteredWindow: 8}
	m.MaxMessageBytes = 64 << 10
	return m.Normalize()
}
