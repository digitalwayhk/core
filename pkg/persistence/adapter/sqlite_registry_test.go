package adapter_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/adapter"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
)

func TestGlobalSqliteInstanceSharedAcrossPackages(t *testing.T) {
	name := fmt.Sprintf("shared-registry-%s", t.Name())
	want := adapter.GetGlobalSqliteInstance(name)
	if got := entity.GetGlobalSqliteInstance(name); got != want {
		t.Fatalf("adapter 与 entity 应共享同一 SQLite 实例: adapter=%p entity=%p", want, got)
	}

	const goroutines = 32
	results := make(chan interface{}, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if index%2 == 0 {
				results <- adapter.GetGlobalSqliteInstance(name)
				return
			}
			results <- entity.GetGlobalSqliteInstance(name)
		}(i)
	}
	wg.Wait()
	close(results)

	for result := range results {
		if result != want {
			t.Fatalf("并发调用返回了不同实例: want=%p got=%p", want, result)
		}
	}
}
