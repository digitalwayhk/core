package config

import (
	"errors"
	"time"
)

// TransportConfig 内部传输配置。Internal 指定首选协议，Fallback 为降级顺序。
type TransportConfig struct {
	Internal   string                `json:",optional"` // grpc | http | socket | quic | mq
	Fallback   []string              `json:",optional"`
	MaxRetries int                   `json:",optional"` // 网络错误重试次数，0=不重试
	RetryDelay time.Duration         `json:",optional"` // 重试基础延迟，默认 100ms
	HTTP       HTTPTransportConfig   `json:",optional"`
	Socket     SocketTransportConfig `json:",optional"`
	QUIC       QUICTransportConfig   `json:",optional"`
	GRPC       GRPCTransportConfig   `json:",optional"`
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
	if t.MaxRetries == 0 {
		t.MaxRetries = 2
	}
	if t.RetryDelay <= 0 {
		t.RetryDelay = 100 * time.Millisecond
	}
	// 默认启用 HTTP（兼容现有调用路径）
	if !t.HTTP.Enable && t.Internal == "" {
		t.HTTP.Enable = true
	}
}

// Validate 校验 TransportConfig 中的字段合法性。
func (t *TransportConfig) Validate() error {
	implementedTransports := map[string]bool{
		"grpc": true, "http": true, "socket": true,
	}
	if t.Internal != "" {
		switch t.Internal {
		case "quic", "mq":
			return errors.New("transport.internal " + t.Internal + " is not implemented; use grpc, http, or socket")
		}
		if !implementedTransports[t.Internal] {
			return errors.New("transport.internal must be one of: grpc, http, socket")
		}
	}
	for _, fb := range t.Fallback {
		switch fb {
		case "quic", "mq":
			return errors.New("transport.fallback contains " + fb + ", which is not implemented; use grpc, http, or socket")
		}
		if !implementedTransports[fb] {
			return errors.New("transport.fallback contains invalid value: " + fb)
		}
	}
	if t.HTTP.Enable {
		return errors.New("transport.http.enable is not implemented; use Transport.Internal/Fallback")
	}
	if t.Socket.Enable {
		return errors.New("transport.socket.enable is not implemented; use Transport.Internal/Fallback")
	}
	if t.GRPC.Enable {
		return errors.New("transport.grpc.enable is not implemented; use Transport.Internal/Fallback")
	}
	if t.QUIC.Enable {
		return errors.New("transport.quic.enable is not implemented; remove it or set it to false")
	}
	if t.QUIC.CertFile != "" {
		return errors.New("transport.quic.certFile is not implemented; remove this field")
	}
	if t.QUIC.KeyFile != "" {
		return errors.New("transport.quic.keyFile is not implemented; remove this field")
	}
	if t.GRPC.Port != 0 && t.GRPC.Port != 19090 {
		return errors.New("transport.grpc.port is not configurable; use 0 or 19090")
	}
	return nil
}
