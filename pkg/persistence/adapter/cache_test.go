package adapter

import (
	"errors"
	"testing"
)

func TestCacheAdapterUnavailableReturnsError(t *testing.T) {
	cache := &CacheAdapter{DbName: "missing"}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "get", run: func() error { _, err := cache.Get("key"); return err }},
		{name: "set", run: func() error { return cache.Set("key", "value", 30) }},
		{name: "delete", run: func() error { return cache.Del("key") }},
		{name: "scan", run: func() error { _, err := cache.Scan(); return err }},
		{name: "search", run: func() error { _, err := cache.Search("prefix"); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, ErrCacheAdapterUnavailable) {
				t.Fatalf("应返回 ErrCacheAdapterUnavailable，实际为 %v", err)
			}
		})
	}
}
