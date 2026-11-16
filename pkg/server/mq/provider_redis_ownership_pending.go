// 本文件只缓存有界PEL身份分类，不保存正文；原登记、PEL及fenced claim仍是执行权威。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
)

type redisOwnedPendingHint struct{ ordering, scope string }

// pendingRows每次重取完整原PEL身份页，删除已消失提示；仅新ID读取正文以分类。
func (r *redisOwnedReader) pendingRows(ctx context.Context, subject string, read bool) ([]redis.XMessage, string, error) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	old := r.pending[subject]
	ids := make([]string, 0, len(old))
	for id := range old {
		ids = append(ids, id)
	}
	rows, next, err := r.provider.claimReliableKeyedPage(ctx, r.provider.streamKey(subject), subject, r.subscribeOptions(), "0-0", r.options.Manifest.PendingWindow(), ids, true)
	if err != nil {
		return nil, next, err
	}
	hints := make(map[string]redisOwnedPendingHint, len(rows))
	var bodies int64
	for i, row := range rows {
		var hint redisOwnedPendingHint
		if redisPendingHint(row) {
			var ok bool
			hint, ok = old[row.ID]
			if !ok {
				return nil, next, ownership.ErrUnknown
			}
		} else {
			bodies++
			if len(redisMessageData(row.Values["data"])) > int(r.options.Manifest.MaxMessageBytes) {
				return nil, next, ownership.ErrCapacity
			}
			hint.ordering = string(redisMessageData(row.Values["ordering_key"]))
			hint.scope, err = r.options.Key(&Message{ID: row.ID, Subject: subject, Data: redisMessageData(row.Values["data"])})
			if err != nil || hint.ordering == "" || hint.scope == "" || len(hint.scope) > 512 {
				return nil, next, ownership.ErrUnknown
			}
		}
		if len(hint.ordering) <= redisPendingHintMaxKeyBytes {
			hints[row.ID] = hint
		}
		rows[i].Values["ordering_key"] = hint.ordering
		rows[i].Values["__core_owned_classification"] = hint
	}
	r.pending[subject] = hints
	if read {
		r.metrics.reclaimRead.Add(bodies)
	}
	return rows, next, nil
}

func (r *redisOwnedReader) pendingScope(subject string, row redis.XMessage) (string, error) {
	if value, ok := row.Values["__core_owned_classification"].(redisOwnedPendingHint); ok {
		return value.scope, nil
	}
	return r.options.Key(&Message{ID: row.ID, Subject: subject, Data: redisMessageData(row.Values["data"])})
}

// hydratePending仅为即将执行的原消息重取正文；失败不调用handler，不ACK，不改变原PEL。
func (r *redisOwnedReader) hydratePending(ctx context.Context, c ownership.Credential, messages []*Message) error {
	for _, message := range messages {
		if message.Data != nil {
			continue
		}
		row, err := r.provider.pendingBody(ctx, r.provider.streamKey(message.Subject), message.ID)
		if err != nil {
			return ownershipRedisError(err)
		}
		if len(redisMessageData(row.Values["data"])) > int(r.options.Manifest.MaxMessageBytes) {
			return ownership.ErrCapacity
		}
		key, err := r.options.Key(&Message{ID: row.ID, Subject: message.Subject, Data: redisMessageData(row.Values["data"])})
		if err != nil || key != c.Key {
			return ownership.ErrUnknown
		}
		message.Data = redisMessageData(row.Values["data"])
	}
	return nil
}
