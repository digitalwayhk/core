package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// GRPCServerNameTargetService makes the gRPC client verify the server
// certificate against PayLoad.TargetService for service-discovered calls.
const GRPCServerNameTargetService = "{service}"

// TransportConfig 内部传输配置。Internal 指定首选协议，Fallback 为降级顺序。
type TransportConfig struct {
	Internal   string              `json:",optional"` // grpc | http | quic | mq
	Fallback   []string            `json:",optional"`
	MaxRetries int                 `json:",optional"` // 网络错误重试次数，0=不重试
	RetryDelay time.Duration       `json:",optional"` // 重试基础延迟，默认 100ms
	HTTP       HTTPTransportConfig `json:",optional"`
	QUIC       QUICTransportConfig `json:",optional"`
	GRPC       GRPCTransportConfig `json:",optional"`
}

// HTTPTransportConfig HTTP 传输配置。
type HTTPTransportConfig struct {
	Enable bool `json:",optional"`
}

// QUICTransportConfig QUIC 传输配置。
type QUICTransportConfig struct {
	Enable   bool   `json:",optional"`
	CertFile string `json:",optional"`
	KeyFile  string `json:",optional"`
}

// GRPCSecurityConfig gRPC 传输安全配置。
type GRPCSecurityConfig struct {
	Mode       string `json:",optional"` // insecure | tls | mtls | mesh
	CAFile     string `json:",optional"`
	CertFile   string `json:",optional"`
	KeyFile    string `json:",optional"`
	ServerName string `json:",optional"`
}

// GRPCTransportConfig gRPC 传输配置。
type GRPCTransportConfig struct {
	Port           int                `json:",optional"`
	MaxRecvMsgSize int                `json:",optional"`
	MaxSendMsgSize int                `json:",optional"`
	Security       GRPCSecurityConfig `json:",optional"`
}

// ApplyDefaults 为 TransportConfig 补充缺失的默认值。
func (t *TransportConfig) ApplyDefaults() {
	if t.Internal == "" {
		t.Internal = "grpc"
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
}

// ApplyServerDefaults 根据服务端口和集群拓扑补充 gRPC 默认值。
func (t *TransportConfig) ApplyServerDefaults(cluster ClusterConfig, httpPort int) {
	if t.GRPC.Port == 0 && httpPort > 0 {
		t.GRPC.Port = httpPort + 10000
	}
	if t.GRPC.Security.Mode != "" || t.GRPC.Security.hasConfiguredFields() {
		return
	}
	if cluster.Mode == "off" || !isExternalClusterProvider(cluster.Provider) {
		t.GRPC.Security.Mode = "insecure"
		return
	}
	t.GRPC.Security.Mode = "mtls"
}

// Validate 校验 TransportConfig 中的字段合法性。
func (t *TransportConfig) Validate() error {
	implementedTransports := map[string]bool{
		"grpc": true, "http": true,
	}
	if t.Internal != "" {
		switch t.Internal {
		case "quic", "mq":
			return errors.New("transport.internal " + t.Internal + " is not implemented; use grpc or http")
		}
		if !implementedTransports[t.Internal] {
			return errors.New("transport.internal must be one of: grpc, http")
		}
	}
	for _, fb := range t.Fallback {
		switch fb {
		case "quic", "mq":
			return errors.New("transport.fallback contains " + fb + ", which is not implemented; use grpc or http")
		}
		if !implementedTransports[fb] {
			return errors.New("transport.fallback contains invalid value: " + fb)
		}
	}
	if t.HTTP.Enable {
		return errors.New("transport.http.enable is not implemented; use Transport.Internal/Fallback")
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
	if t.GRPC.Port < 0 || t.GRPC.Port > 65535 {
		return fmt.Errorf("Transport.GRPC.Port must be 0 or between 1 and 65535, got %d", t.GRPC.Port)
	}
	if err := t.GRPC.Security.validate(); err != nil {
		return err
	}
	return nil
}

// ValidateForServer 校验需要服务端集群上下文的 gRPC 约束。
func (t *TransportConfig) ValidateForServer(cluster ClusterConfig, runIP string) error {
	if cluster.Mode == "off" || !isExternalClusterProvider(cluster.Provider) || t.GRPC.Security.Mode != "insecure" {
		return nil
	}
	address := strings.TrimSpace(cluster.AdvertiseAddress)
	if address == "" {
		address = strings.TrimSpace(runIP)
	}
	if !isLoopbackAddress(address) {
		return fmt.Errorf("Transport.GRPC.Security.Mode: insecure grpc is limited to loopback for external provider %q", cluster.Provider)
	}
	return nil
}

func (s GRPCSecurityConfig) validate() error {
	switch s.Mode {
	case "":
		if s.hasConfiguredFields() {
			return errors.New("Transport.GRPC.Security.Mode is required when security fields are configured")
		}
		return nil
	case "tls":
		if s.CertFile == "" {
			return errors.New("Transport.GRPC.Security.CertFile is required when Mode=tls")
		}
		if s.KeyFile == "" {
			return errors.New("Transport.GRPC.Security.KeyFile is required when Mode=tls")
		}
	case "mtls":
		if s.CAFile == "" {
			return errors.New("Transport.GRPC.Security.CAFile is required when Mode=mtls")
		}
		if s.CertFile == "" {
			return errors.New("Transport.GRPC.Security.CertFile is required when Mode=mtls")
		}
		if s.KeyFile == "" {
			return errors.New("Transport.GRPC.Security.KeyFile is required when Mode=mtls")
		}
	case "insecure", "mesh":
		if s.CAFile != "" {
			return fmt.Errorf("Transport.GRPC.Security.CAFile must be empty when Mode=%s", s.Mode)
		}
		if s.CertFile != "" {
			return fmt.Errorf("Transport.GRPC.Security.CertFile must be empty when Mode=%s", s.Mode)
		}
		if s.KeyFile != "" {
			return fmt.Errorf("Transport.GRPC.Security.KeyFile must be empty when Mode=%s", s.Mode)
		}
		if s.ServerName != "" {
			return fmt.Errorf("Transport.GRPC.Security.ServerName must be empty when Mode=%s", s.Mode)
		}
	default:
		return fmt.Errorf("Transport.GRPC.Security.Mode=%q is invalid; use insecure, tls, mtls, or mesh", s.Mode)
	}
	return nil
}

func (s GRPCSecurityConfig) hasConfiguredFields() bool {
	return s.CAFile != "" || s.CertFile != "" || s.KeyFile != "" || s.ServerName != ""
}

func isExternalClusterProvider(provider string) bool {
	switch provider {
	case "redis", "etcd", "consul":
		return true
	default:
		return false
	}
}

func isLoopbackAddress(address string) bool {
	if strings.EqualFold(address, "localhost") {
		return true
	}
	if addr, err := netip.ParseAddr(address); err == nil {
		return addr.IsLoopback()
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}
