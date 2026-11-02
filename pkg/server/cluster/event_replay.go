// 本文件在已有归属Gateway心跳中重放现存订阅摘要，恢复新启动peer的正式中继索引。
package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
)

// ReplayLocalSubscriptions 复用现存会话索引与正式中继；每次最多128条摘要且受ctx约束。
// 不按peerID永久缓存成功，因此同标识/同地址的新进程也能恢复。调用方复用原有心跳。
// 持读锁到摘要发送结束，并发退订的false必定在本次true之后发出。
func (b *CrossNodeNoticeBroker) ReplayLocalSubscriptions(ctx context.Context) error {
	b.stopMu.Lock()
	stopped := b.stopped
	b.stopMu.Unlock()
	if stopped {
		return nil
	}
	b.replayMu.Lock()
	defer b.replayMu.Unlock()
	nodes, err := b.provider.List(ctx, b.serviceName, NodeStatusRunning)
	if err != nil {
		return err
	}
	peers := make([]*NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.ID != b.localNodeID {
			peers = append(peers, node)
		}
	}
	if len(peers) > 1024 {
		return fmt.Errorf("subscription replay capacity reached")
	}
	if len(peers) == 0 {
		return nil
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	b.localSubsMu.RLock()
	defer b.localSubsMu.RUnlock()
	type subscription struct {
		path string
		hash uint64
	}
	subscriptions := make([]subscription, 0)
	for path, hashes := range b.localSubs {
		for hash := range hashes {
			subscriptions = append(subscriptions, subscription{path, hash})
			if len(subscriptions) > 1024 {
				return fmt.Errorf("subscription replay capacity reached")
			}
		}
	}
	if len(subscriptions) == 0 {
		return nil
	}
	sort.Slice(subscriptions, func(i, j int) bool {
		a, c := subscriptions[i], subscriptions[j]
		if a.path == c.path {
			return a.hash < c.hash
		}
		return a.path < c.path
	})
	total := len(peers) * len(subscriptions)
	var result error
	for work := 0; work < min(128, total); work++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		index := b.replayCursor % total
		b.replayCursor = (index + 1) % total
		node := peers[index%len(peers)]
		item := subscriptions[index/len(peers)]
		raw, err := json.Marshal(subscriptionSummaryPayload{RoutePath: item.path, Hash: item.hash, NodeID: b.localNodeID, Active: true})
		if err != nil {
			return err
		}
		if b.sender != nil {
			_, err = b.sender(ctx, node, raw, "/api/servermanage/ws/subscription")
		} else {
			url := "http://" + net.JoinHostPort(node.Address, strconv.Itoa(node.Port)) + "/api/servermanage/ws/subscription"
			req, e := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
			if e != nil {
				return e
			}
			req.Header.Set("Content-Type", "application/json")
			resp, e := b.httpClient.Do(req)
			err = e
			if resp != nil {
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					err = fmt.Errorf("subscription replay rejected")
				}
			}
		}
		result = errors.Join(result, err)
	}
	return result
}
