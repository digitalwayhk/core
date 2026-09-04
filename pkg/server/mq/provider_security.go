package mq

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"

	"github.com/digitalwayhk/core/pkg/server/config"
)

// buildMQTLSConfig 从公共 MQ TLS 配置构造严格校验的客户端 TLS 配置。
func buildMQTLSConfig(cfg config.MQTLSConfig) (*tls.Config, error) {
	if !cfg.Enable {
		return nil, nil
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return nil, fmt.Errorf("mq tls: CertFile and KeyFile must be configured together")
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if cfg.CAFile != "" {
		pemData, readErr := os.ReadFile(cfg.CAFile)
		if readErr != nil {
			return nil, fmt.Errorf("mq tls: read CAFile: %w", readErr)
		}
		if ok := rootCAs.AppendCertsFromPEM(pemData); !ok {
			return nil, fmt.Errorf("mq tls: parse CAFile: no certificates found")
		}
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
		ServerName: cfg.ServerName,
	}
	if cfg.CertFile != "" {
		certificate, loadErr := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if loadErr != nil {
			return nil, fmt.Errorf("mq tls: load client certificate: %w", loadErr)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

// redactedAMQPURL 移除 URL userinfo，供安全错误上下文使用。
func redactedAMQPURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid-amqp-url>"
	}
	parsed.User = nil
	return parsed.String()
}
