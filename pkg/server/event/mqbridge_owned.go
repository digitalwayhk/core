// 本文件只转换既有Envelope和公共凭据，不让应用读取Redis键或自建消费循环。
package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
)

// SubscribeOwned 由正式Bridge创建独立消费会话；批次成功后由Provider fenced ACK。
func (b *MQBridge) SubscribeOwned(ctx context.Context, sub OwnedSubscription, instanceID string) (*ownership.Runtime, error) {
	if b == nil || b.manager == nil {
		return nil, ownership.ErrUnsupported
	}
	if sub.Key == nil || sub.Handler == nil {
		return nil, ownership.ErrUnknown
	}
	if err := b.EnsureSharedKeyReliable(); err != nil {
		return nil, err
	}
	var session [16]byte
	if _, err := rand.Read(session[:]); err != nil {
		return nil, err
	}
	decode := func(msg *mq.Message) (*Envelope, error) {
		var env Envelope
		err := json.Unmarshal(msg.Data, &env)
		if err != nil {
			return nil, err
		}
		if env.Subject != msg.Subject || env.ShardKey == "" {
			return nil, ownership.ErrUnknown
		}
		return &env, nil
	}
	return b.manager.SubscribeOwnedBatch(ctx, mq.OwnedReliableOptions{Manifest: sub.Manifest, InstanceID: instanceID, SessionID: hex.EncodeToString(session[:]), MaxOwned: sub.MaxOwned, Activate: sub.Activate, Restore: sub.Restore, Due: sub.Due, NoDueTasks: sub.NoDueTasks, Key: func(msg *mq.Message) (string, error) {
		env, err := decode(msg)
		if err != nil {
			return "", err
		}
		return sub.Key(env)
	}}, func(ctx context.Context, c ownership.Credential, messages []*mq.Message) error {
		envelopes := make([]*Envelope, 0, len(messages))
		for _, msg := range messages {
			env, err := decode(msg)
			if err != nil {
				return err
			}
			envelopes = append(envelopes, env)
		}
		return sub.Handler(ctx, c, envelopes)
	})
}
