// 本文件复用SharedKeys的原组有界PEL/新读取；按稳定范围键形成真实批次。
package mq

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"sort"
	"sync"
	"time"
)

type redisOwnedReader struct {
	provider *RedisStreamProvider
	options  OwnedReliableOptions
	handler  func(context.Context, ownership.Credential, []*Message) error
	metrics  *redisReliableSubscriptionMetrics
	mu       sync.Mutex
	cursors  map[string]int
	inflight map[string]int64
}

func newRedisOwnedReader(p *RedisStreamProvider, o OwnedReliableOptions, h func(context.Context, ownership.Credential, []*Message) error, m *redisReliableSubscriptionMetrics) *redisOwnedReader {
	return &redisOwnedReader{provider: p, options: o, handler: h, metrics: m, cursors: map[string]int{}, inflight: map[string]int64{}}
}
func (r *redisOwnedReader) subscribeOptions() ReliableSubscribeOptions {
	return ReliableSubscribeOptions{Group: r.options.Manifest.Group, Consumer: r.options.InstanceID + ":" + r.options.SessionID, SharedKeys: true, KeyConcurrency: 1, Count: r.options.Manifest.Window, scopeMarker: ownedRedisMarker(r.options.Manifest), scopeMessageLimit: r.options.Manifest.MaxMessageBytes}
}

func (r *redisOwnedReader) subjectMessages(ctx context.Context, subject string, read bool) ([]redis.XMessage, error) {
	p := r.provider
	m := r.options.Manifest
	options := r.subscribeOptions()
	key := p.streamKey(subject)
	scanStarted := time.Now()
	rows, _, err := p.claimReliableKeyed(ctx, key, subject, options, "0-0", m.Window)
	if err != nil {
		return nil, ownershipRedisError(err)
	}
	if read {
		observeRedisKeyedRead(ctx, scanStarted, time.Now(), "pending_scan", key, m.Group, rows)
		r.metrics.reclaimRead.Add(int64(len(rows)))
	}
	if read {
		pending, err := p.client.XPending(ctx, key, m.Group).Result()
		if err != nil {
			return nil, err
		}
		if pending.Count < m.Window {
			readStarted := time.Now()
			entries, err := p.readReliableKeyed(ctx, key, subject, options, m.Window-pending.Count)
			if err != nil && !errors.Is(err, redis.Nil) {
				return nil, ownershipRedisError(err)
			}
			if len(entries) > 0 {
				readAt := time.Now()
				for _, stream := range entries {
					observeRedisKeyedRead(ctx, readStarted, readAt, "new", key, m.Group, stream.Messages)
					r.metrics.newRead.Add(int64(len(stream.Messages)))
					for _, row := range stream.Messages {
						r.metrics.publishToReadNanos.Add(redisMessageAge(row.ID, readAt).Nanoseconds())
					}
				}
				// 其他实例可能在首次PEL扫描与新读取之间推进游标；新读取只预取，
				// 必须重新扫描最早PEL，不能直接准入本次返回的后序消息。
				rows, _, err = p.claimReliableKeyed(ctx, key, subject, options, "0-0", m.Window)
				if err != nil {
					return nil, ownershipRedisError(err)
				}
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return streamIDCompare(rows[i].ID, rows[j].ID) < 0 })
	return rows, nil
}

func (r *redisOwnedReader) selectMessages(subject, key string, rows []redis.XMessage) ([]*Message, error) {
	return r.mapMessages(subject, key, rows, true)
}

// 调度只准入最早前缀；观测必须连同被前序阻挡的工作一起计数。
func (r *redisOwnedReader) mapMessages(subject, key string, rows []redis.XMessage, ordered bool) ([]*Message, error) {
	known := map[string]bool{}
	for _, key := range r.options.Manifest.Keys {
		known[key] = true
	}
	orderingScopes := map[string]string{}
	blocked := map[string]bool{}
	result := make([]*Message, 0, len(rows))
	for _, row := range rows {
		ordering := string(redisMessageData(row.Values["ordering_key"]))
		if ordering == "" {
			return nil, ownership.ErrUnknown
		}
		msg := &Message{ID: row.ID, Subject: subject, Data: redisMessageData(row.Values["data"]), Ack: func() error { return errors.New("ownership: ack only after batch success") }}
		scopeKey, err := r.options.Key(msg)
		if err != nil || !known[scopeKey] {
			return nil, ownership.ErrUnknown
		}
		if previous, ok := orderingScopes[ordering]; ordered && ok && previous != scopeKey {
			blocked[ordering] = true
		}
		if blocked[ordering] {
			continue
		}
		orderingScopes[ordering] = scopeKey
		if scopeKey == key {
			result = append(result, msg)
		}
	}
	return result, nil
}

func (r *redisOwnedReader) step(ctx context.Context, c ownership.Credential) (bool, error) {
	m := r.options.Manifest
	r.mu.Lock()
	start := r.cursors[c.Key]
	r.cursors[c.Key] = (start + 1) % len(m.Subjects)
	r.mu.Unlock()
	var messages []*Message
	for i := 0; i < len(m.Subjects); i++ {
		subject := m.Subjects[(start+i)%len(m.Subjects)]
		rows, err := r.subjectMessages(ctx, subject, true)
		if err != nil {
			return false, err
		}
		selected, err := r.selectMessages(subject, c.Key, rows)
		if err != nil {
			return false, err
		}
		remaining := m.MaxBatch - len(messages)
		if len(selected) > remaining {
			selected = selected[:remaining]
		}
		messages = append(messages, selected...)
	}
	if len(messages) == 0 {
		if r.options.Due == nil {
			return false, nil
		}
		due, err := r.options.Due(ctx, c)
		if err != nil {
			return false, err
		}
		if due < 0 {
			return false, ownership.ErrUnknown
		}
		if due == 0 {
			return false, nil
		}
	}
	if err := r.claim(ctx, c, messages); err != nil {
		return false, err
	}
	r.mu.Lock()
	r.inflight[c.Key] = int64(len(messages))
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.inflight, c.Key); r.mu.Unlock() }()
	if err := r.runBatch(ctx, c, messages); err != nil {
		return false, err
	}
	return true, nil
}

func (r *redisOwnedReader) observe(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
	o := ownership.Observation{At: time.Now()}
	m := r.options.Manifest
	for _, subject := range m.Subjects {
		rows, err := r.subjectMessages(ctx, subject, false)
		if err != nil {
			return o, err
		}
		selected, err := r.mapMessages(subject, c.Key, rows, false)
		if err != nil {
			return o, err
		}
		if len(selected) > 0 {
			o.WorkVisible = true
		}
		pending, err := r.provider.client.XPending(ctx, r.provider.streamKey(subject), m.Group).Result()
		if err != nil {
			return o, err
		}
		if pending.Count > int64(len(rows)) {
			o.Delivered = -1
		} else if o.Delivered >= 0 {
			o.Delivered += int64(len(selected))
		}
		groups, err := r.provider.client.XInfoGroups(ctx, r.provider.streamKey(subject)).Result()
		if err != nil {
			return o, err
		}
		last := ""
		for _, group := range groups {
			if group.Name == m.Group {
				last = group.LastDeliveredID
			}
		}
		if last == "" {
			return o, ownership.ErrUnknown
		}
		unread, err := r.unread(ctx, subject, last)
		if err != nil {
			return o, err
		}
		if int64(len(unread)) > m.Window {
			visible, err := r.mapMessages(subject, c.Key, unread[:m.Window], false)
			if err != nil {
				return o, err
			}
			if len(visible) > 0 {
				o.WorkVisible = true
			}
			o.Unread = -1
		} else {
			selected, err := r.mapMessages(subject, c.Key, unread, false)
			if err != nil {
				return o, err
			}
			if o.Unread >= 0 {
				o.Unread += int64(len(selected))
			}
		}
	}
	r.mu.Lock()
	inflight := r.inflight[c.Key]
	r.mu.Unlock()
	if o.Delivered < 0 {
		o.Queued = -1
	} else {
		o.Queued = max(0, o.Delivered-inflight)
	}
	if r.options.Due != nil {
		due, err := r.options.Due(ctx, c)
		if err != nil {
			return o, err
		}
		if due < 0 {
			return o, ownership.ErrUnknown
		}
		o.Due = due
	}
	return o, nil
}
