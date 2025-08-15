package config

import "time"

type MelodyConfig struct {
	WriteWait                 time.Duration // Duration until write times out.
	PongWait                  time.Duration // Timeout for waiting on pong.
	PingPeriod                time.Duration // Duration between pings.
	MaxMessageSize            int64         // Maximum size in bytes of a message.
	MessageBufferSize         int           // The max amount of messages that can be in a sessions buffer before it starts dropping them.
	ConcurrentMessageHandling bool          // Handle messages from sessions concurrently.
	MaxConnections            int64         // 最大连接数
}

func NewMelodyConfig() *MelodyConfig {
	return &MelodyConfig{
		WriteWait:                 10 * time.Second,
		PongWait:                  60 * time.Second,
		PingPeriod:                54 * time.Second,
		MaxMessageSize:            512,
		MessageBufferSize:         256,
		ConcurrentMessageHandling: true,
		MaxConnections:            10000, // 默认最大连接数
	}
}

type MelodyConfigOption func(con *MelodyConfig)

func GetMelodyConfig() *MelodyConfig {
	config := NewMelodyConfig()
	return config
}
