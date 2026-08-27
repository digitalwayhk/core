package mq

import (
	"context"
	"sync"
)

type reliableKeyedWork struct {
	key string
	id  string
	run func() error
}

type reliableKeyedFailure struct {
	work reliableKeyedWork
	err  error
}

// admitReliableKeyedWork 对一个 admission window 限制每 key 数量，并跳过仍被
// 最早失败消息阻断的 key。items 的稳定输入顺序保持不变。
func admitReliableKeyedWork(
	items []reliableKeyedWork,
	blocked map[string]reliableKeyedWork,
	perKeyLimit int,
) []reliableKeyedWork {
	if perKeyLimit <= 0 {
		perKeyLimit = 1
	}
	counts := make(map[string]int)
	result := make([]reliableKeyedWork, 0, len(items))
	for _, item := range items {
		if _, skip := blocked[item.key]; skip {
			continue
		}
		if counts[item.key] >= perKeyLimit {
			continue
		}
		counts[item.key]++
		result = append(result, item)
	}
	return result
}

// runReliableKeyedBatch 在一个 batch 内同 key 串行、不同 key 有界并行。
// 任一 work 失败只停止该 lane，返回的 failure 是该 key 最早失败消息。
func runReliableKeyedBatch(
	ctx context.Context,
	concurrency int,
	items []reliableKeyedWork,
) []reliableKeyedFailure {
	if concurrency <= 0 {
		concurrency = 1
	}
	lanes := make(map[string][]reliableKeyedWork)
	keys := make([]string, 0)
	for _, item := range items {
		if _, exists := lanes[item.key]; !exists {
			keys = append(keys, item.key)
		}
		lanes[item.key] = append(lanes[item.key], item)
	}
	if len(keys) == 0 {
		return nil
	}
	if concurrency > len(keys) {
		concurrency = len(keys)
	}

	semaphore := make(chan struct{}, concurrency)
	failures := make(chan reliableKeyedFailure, len(keys))
	var wg sync.WaitGroup
	for _, key := range keys {
		lane := lanes[key]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				failures <- reliableKeyedFailure{work: lane[0], err: ctx.Err()}
				return
			}
			for _, item := range lane {
				if item.run == nil {
					continue
				}
				if err := item.run(); err != nil {
					failures <- reliableKeyedFailure{work: item, err: err}
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)

	result := make([]reliableKeyedFailure, 0, len(failures))
	for failure := range failures {
		result = append(result, failure)
	}
	return result
}
