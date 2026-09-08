// 本程序用同一源码分别链接旧版和候选 Core，验证真实 Broker 原地接管。
package main

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"os"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func wait(ctx context.Context, fn func() bool) {
	for !fn() {
		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func main() {
	// 默认 durable AckWait 为 30 秒；这是测试总恢复窗口，不是单轮 100 ms 回收预算。
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var p mq.LifecycleConformanceProvider
	prefix := os.Getenv("MIGRATION_PREFIX")
	if prefix == "" {
		panic("missing isolated prefix")
	}
	if os.Args[1] == "redis" {
		p = mq.NewRedisStreamProvider("127.0.0.1:52954", prefix, 0)
	} else {
		p = mq.NewNATSJetStreamProvider("nats://127.0.0.1:52959", prefix, prefix)
	}
	must(p.Connect(ctx))
	defer p.Close()
	policy := mq.LifecyclePolicy{Subject: "fills", Mode: mq.LifecycleModeEnforce, RequiredGroups: []mq.ConsumerGroupRequirement{{Name: "primary", Start: mq.StartFromAllRetained}, {Name: "offline", Start: mq.StartFromAllRetained}}, Reclaim: mq.ReclaimBudget{Interval: 2 * time.Second, BatchSize: 2000, TimeBudget: 100 * time.Millisecond}}.Normalize()
	if os.Args[2] == "seed" {
		must(p.EnsureLifecycle(ctx, policy))
		must(p.Publish(ctx, "fills", []byte("completed-primary"), nil))
		stop, err := p.SubscribeReliable(ctx, "fills", mq.ReliableSubscribeOptions{Group: "primary", Consumer: "old", ClaimInterval: 50 * time.Millisecond}, func(*mq.Message) error { return nil })
		must(err)
		wait(ctx, func() bool {
			s, e := p.InspectLifecycle(ctx, policy)
			return e == nil && s.PendingMessages != nil && *s.PendingMessages == 0 && s.Groups[1].CompletedFrontier != "0" && s.Groups[1].CompletedFrontier != "0-0"
		})
		stop()
		must(p.Publish(ctx, "fills", []byte("pending-primary"), nil))
		stop, err = p.SubscribeReliable(ctx, "fills", mq.ReliableSubscribeOptions{Group: "primary", Consumer: "old-failed", ClaimInterval: 50 * time.Millisecond}, func(*mq.Message) error { return fmt.Errorf("fixture pending") })
		must(err)
		wait(ctx, func() bool {
			s, e := p.InspectLifecycle(ctx, policy)
			return e == nil && s.PendingMessages != nil && *s.PendingMessages > 0
		})
		stop()
		s, e := p.InspectLifecycle(ctx, policy)
		must(e)
		fmt.Printf("seed fingerprint=%s retained=%d pending=%d\n", policy.Fingerprint(), *s.RetainedMessages, *s.PendingMessages)
	} else {
		must(p.EnsureLifecycle(ctx, policy))
		s, e := p.InspectLifecycle(ctx, policy)
		must(e)
		if s.RetainedMessages == nil || *s.RetainedMessages != 2 || s.PendingMessages == nil || *s.PendingMessages < 1 {
			panic("old data not preserved")
		}
		manager := mq.NewManager()
		manager.Register(p)
		must(manager.SetCurrent(p.Name()))
		defer manager.Close()
		must(manager.RequireMessageLifecycle(ctx, policy))
		for _, group := range []string{"primary", "offline"} {
			stop, e := manager.SubscribeReliable(ctx, "fills", mq.ReliableSubscribeOptions{Group: group, Consumer: "new-" + group, ClaimInterval: 50 * time.Millisecond}, func(*mq.Message) error { return nil })
			must(e)
			defer stop()
		}
		wait(ctx, func() bool {
			s, e := p.InspectLifecycle(ctx, policy)
			return e == nil && s.RetainedMessages != nil && *s.RetainedMessages == 0
		})
		fmt.Printf("upgrade fingerprint=%s preserved=2 pending>=1 recovered_and_reclaimed=true\n", policy.Fingerprint())
	}
}
