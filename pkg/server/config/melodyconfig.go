package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type MelodyConfig struct {
	WriteWait                 time.Duration // Duration until write times out.
	PongWait                  time.Duration // Timeout for waiting on pong.
	PingPeriod                time.Duration // Duration between pings.
	ReadTimeout               time.Duration `json:"read_timeout"` // 新增
	MaxMessageSize            int64         // Maximum size in bytes of a message.
	MessageBufferSize         int           // The max amount of messages that can be in a sessions buffer before it starts dropping them.
	ConcurrentMessageHandling bool          // Handle messages from sessions concurrently.
	MaxConnections            int64         // 最大连接数
}

func NewMelodyConfig() *MelodyConfig {
	return &MelodyConfig{
		WriteWait:                 10 * time.Second, // 写入等待时间
		PongWait:                  60 * time.Second, // 等待pong响应的时间
		PingPeriod:                54 * time.Second, // 发送ping的间隔（应该 < PongWait）
		ReadTimeout:               65 * time.Second, // 稍微大于PongWait
		MaxMessageSize:            1024 * 256,       // 最大消息大小2MB
		MessageBufferSize:         256,
		ConcurrentMessageHandling: true,
		MaxConnections:            10000, // 默认最大连接数
	}
}

var defaultMelodyConfig = NewMelodyConfig()

type MelodyConfigOption func(con *MelodyConfig)

func GetMelodyConfig() *MelodyConfig {
	return defaultMelodyConfig
}

func loadMelodyConfig(configPath string) error {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, defaultMelodyConfig)
	if err != nil {
		return err
	}
	return nil
}
