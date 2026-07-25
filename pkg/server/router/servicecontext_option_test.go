package router

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func TestServiceContextServerOptionConcurrentAccess(t *testing.T) {
	name := fmt.Sprintf("server-option-%d", time.Now().UnixNano())
	ctx := &ServiceContext{Config: &config.ServerConfig{RemoteAccessManageAPI: true}}
	const workers = 32
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wait.Done()
			<-start
			ctx.SetServerOption(&types.ServerOption{
				OriginCors: []string{fmt.Sprintf("https://%s-%d.example.com", name, worker)},
				Trans:      &types.TransOption{RetryCount: worker},
			})
			got := ctx.GetServerOption()
			if got == nil {
				t.Errorf("worker %d received nil option", worker)
				return
			}
			got.OriginCors[0] = "mutated"
			got.Trans.RetryCount = -1
		}()
	}
	close(start)
	wait.Wait()

	got := ctx.GetServerOption()
	if got == nil || got.Trans == nil || got.Trans.RetryCount < 0 || got.OriginCors[0] == "mutated" {
		t.Fatalf("returned option exposed internal state: %#v", got)
	}
	if !got.RemoteAccessManageAPI {
		t.Fatal("RemoteAccessManageAPI was not copied from context config")
	}
}
