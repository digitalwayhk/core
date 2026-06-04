package config

import "errors"

// TransportConfig 内部传输配置。Internal 指定首选协议，Fallback 为降级顺序。
type TransportConfig struct {
	Internal string              `json:",optional"` // grpc | http | socket | quic | mq
	Fallback []string            `json:",optional"`
	HTTP     HTTPTransportConfig  `json:",optional"`
	Socket   SocketTransportConfig `json:",optional"`
	QUIC     QUICTransportConfig  `json:",optional"`
	GRPC     GRPCTransportConfig  `json:",optional"`
}

// HTTPTransportConfig HTTP 传输配置。
type HTTPTransportConfig struct {
	Enable bool `json:",optional"`
}

// SocketTransportConfig 内部 Socket 传输配置。
type SocketTransportConfig struct {
	Enable bool `json:",optional"`
}

// QUICTransportConfig QUIC 传输配置。
type QUICTransportConfig struct {
	Enable   bool   `json:",optional"`
	CertFile string `json:",optional"`
	KeyFile  string `json:",optional"`
}

// GRPCTransportConfig gRPC 传输配置。
type GRPCTransportConfig struct {
	Enable         bool `json:",optional"`
	Port           int  `json:",optional"`
	MaxRecvMsgSize int  `json:",optional"`
	MaxSendMsgSize int  `json:",optional"`
}

// ApplyDefaults 为 TransportConfig 补充缺失的默认值。
func (t *TransportConfig) ApplyDefaults() {
	if t.Internal == "" {
		t.Internal = "grpc"
	}
	if len(t.Fallback) == 0 {
		t.Fallback = []string{"grpc", "http", "socket"}
	}
	if t.GRPC.Port == 0 {
		t.GRPC.Port = 19090
	}
	if t.GRPC.MaxRecvMsgSize == 0 {
		t.GRPC.MaxRecvMsgSize = 4 * 1024 * 1024 // 4MB
	}
	if t.GRPC.MaxSendMsgSize == 0 {
		t.GRPC.MaxSendMsgSize = 4 * 1024 * 1024
	}
	// 默认启用 HTTP（兼容现有调用路径）
	if !t.HTTP.Enable && t.Internal == "" {
		t.HTTP.Enable = true
	}
}

// Validate 校验 TransportConfig 中的字段合法性。
func (t *TransportConfig) Validate() error {
	validTransports := map[string]bool{
		"grpc": true, "http": true, "socket": true, "quic": true, "mq": true,
	}
	if t.Internal != "" {
		if !validTransports[t.Internal] {
			return errors.New("transport.internal must be one of: grpc, http, socket, quic, mq")
		}
	}
	for _, fb := range t.Fallback {
		if !validTransports[fb] {
			return errors.New("transport.fallback contains invalid value: " + fb)
		}
	}
	if t.GRPC.Port < 0 || t.GRPC.Port > 65535 {
		return errors.New("transport.grpc.port must be between 0 and 65535")
	}
	return nil
}
