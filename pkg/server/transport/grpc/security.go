package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/zeromicro/go-zero/zrpc"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/digitalwayhk/core/pkg/server/config"
)

// clientSecurityOptions returns only application-level transport security options.
// insecure and mesh rely on grpc-go plaintext transport; mesh encryption is owned by the sidecar.
func clientSecurityOptions(security config.GRPCSecurityConfig) ([]zrpc.ClientOption, error) {
	switch security.Mode {
	case "", "insecure", "mesh":
		return nil, nil
	case "tls", "mtls":
		tlsConfig, err := loadClientTLSConfig(security)
		if err != nil {
			return nil, err
		}
		return []zrpc.ClientOption{zrpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))}, nil
	default:
		return nil, fmt.Errorf("Transport.GRPC.Security.Mode=%q is invalid", security.Mode)
	}
}

func loadClientTLSConfig(security config.GRPCSecurityConfig) (*tls.Config, error) {
	rootCAs, err := loadRootCAs(security.CAFile)
	if err != nil {
		return nil, err
	}
	certificate, err := loadTLSCertificate(security.CertFile, security.KeyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{certificate},
		ServerName:   security.ServerName,
	}, nil
}

// serverSecurityOptions builds only application-level gRPC server credentials.
// mesh encryption belongs to the sidecar, so mesh and insecure intentionally
// leave grpc-go in plaintext mode.
func serverSecurityOptions(security config.GRPCSecurityConfig) ([]googlegrpc.ServerOption, error) {
	switch security.Mode {
	case "", "insecure", "mesh":
		return nil, nil
	case "tls", "mtls":
		certificate, err := loadTLSCertificate(security.CertFile, security.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		}
		if security.Mode == "mtls" {
			clientCAs, err := loadRequiredCertPool(security.CAFile)
			if err != nil {
				return nil, err
			}
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			tlsConfig.ClientCAs = clientCAs
		}
		return []googlegrpc.ServerOption{googlegrpc.Creds(credentials.NewTLS(tlsConfig))}, nil
	default:
		return nil, fmt.Errorf("Transport.GRPC.Security.Mode=%q is invalid", security.Mode)
	}
}

func loadRootCAs(path string) (*x509.CertPool, error) {
	if path == "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("Transport.GRPC.Security.CAFile: load system roots: %w", err)
		}
		return pool, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Transport.GRPC.Security.CAFile: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("Transport.GRPC.Security.CAFile: no valid certificates")
	}
	return pool, nil
}

func loadRequiredCertPool(path string) (*x509.CertPool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Transport.GRPC.Security.CAFile: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("Transport.GRPC.Security.CAFile: no valid certificates")
	}
	return pool, nil
}

func loadTLSCertificate(certPath, keyPath string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("Transport.GRPC.Security.CertFile: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("Transport.GRPC.Security.KeyFile: %w", err)
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("Transport.GRPC.Security.CertFile/KeyFile: %w", err)
	}
	return certificate, nil
}
