// Package controlnotify 只承载 Core 可由权威状态恢复的内部瞬时通知，不承载业务事实。
package controlnotify

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
)

const (
	maxPayloadBytes   = 64 << 10
	operationTimeout  = 3 * time.Second
	heartbeatInterval = time.Second
)

// ErrUnavailable 表示通知连续性无法确认，调用者必须保护状态并重新同步权威。
var ErrUnavailable = errors.New("internal notification channel unavailable")

// Transport 是框架内部的单主题广播连接；Receive 只允许单个读取者。
// 接收失败后不得继续使用旧连接宣告健康，应关闭并重新建立后执行权威对账。
type Transport interface {
	Publish(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
	Close() error
}

// Open 订阅一个框架内置通知主题；ctx 只约束初始化，不拥有连接生命周期。
func Open(ctx context.Context, cfg config.MQConfig, service, kind string) (Transport, error) {
	if ctx == nil {
		return nil, errors.New("internal notification context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Mode == "off" {
		return nil, errors.New("internal notification requires enabled MQ configuration")
	}
	if kind != "cache" && kind != "identity" {
		return nil, errors.New("unknown internal notification kind")
	}
	if strings.TrimSpace(service) == "" || len(service) > 256 {
		return nil, errors.New("invalid internal notification service")
	}
	cfg.ApplyDefaults()
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	switch cfg.Provider {
	case "redis-stream":
		if len(cfg.RedisStream.Prefix) > 256 {
			return nil, errors.New("internal notification prefix too long")
		}
		return openRedis(ctx, cfg.RedisStream, channelName(cfg, service, kind))
	case "nats-jetstream":
		if len(cfg.NATSJetStream.StreamPrefix) > 256 {
			return nil, errors.New("internal notification prefix too long")
		}
		return openNATS(ctx, cfg.NATSJetStream, channelName(cfg, service, kind))
	default:
		return nil, fmt.Errorf("internal notification provider %q is unsupported", cfg.Provider)
	}
}

func channelName(cfg config.MQConfig, service, kind string) string {
	prefix := cfg.NATSJetStream.StreamPrefix
	db := "nats"
	if cfg.Provider == "redis-stream" {
		prefix = cfg.RedisStream.Prefix
		db = "db" + strconv.Itoa(cfg.RedisStream.DB)
	}
	// 编码防止分隔符/通配符注入；Redis Pub/Sub 本身不按 DB 隔离。
	return "_core.notify.v1." + hex.EncodeToString([]byte(prefix)) + "." + db + "." + hex.EncodeToString([]byte(service)) + "." + kind
}

func validatePayload(data []byte) error {
	if len(data) == 0 || len(data) > maxPayloadBytes {
		return errors.New("internal notification payload size invalid")
	}
	return nil
}
