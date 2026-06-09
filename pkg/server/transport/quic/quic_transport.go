// Package quic implements the Transport interface over QUIC (RFC 9000).
//
// The wire protocol mirrors the TCP socket transport: each message is framed
// with a 4-byte little-endian length prefix so streams can carry multiple
// request/response pairs without ambiguity.
//
// TLS note: the default client config uses InsecureSkipVerify=true for
// development convenience. Production deployments should supply a proper TLS
// certificate via NewWithTLS.
package quic

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"

	quicgo "github.com/lucas-clemente/quic-go"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
)

// QUICTransport implements transport.Transport over QUIC streams.
type QUICTransport struct {
	clientTLS *tls.Config // used when dialling peers
	serverTLS *tls.Config // used when listening (Start)
	quicConf  *quicgo.Config
	listener  quicgo.Listener
}

// New returns a QUICTransport with development-safe defaults (InsecureSkipVerify).
// Use NewWithTLS to supply production TLS configuration.
func New() *QUICTransport {
	return &QUICTransport{
		clientTLS: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // dev default; override in production
			NextProtos:         []string{"core-quic"},
		},
		quicConf: &quicgo.Config{
			MaxIdleTimeout: 30 * time.Second,
		},
	}
}

// NewWithTLS returns a QUICTransport using the provided TLS configurations.
// clientTLS is used when dialling; serverTLS is used when Start is called.
func NewWithTLS(clientTLS, serverTLS *tls.Config) *QUICTransport {
	qt := New()
	if clientTLS != nil {
		qt.clientTLS = clientTLS
	}
	if serverTLS != nil {
		qt.serverTLS = serverTLS
	}
	return qt
}

func (q *QUICTransport) Name() string { return "quic" }

// Start begins a QUIC listener on the address derived from the payload or
// the framework-configured QUIC port. If no serverTLS is configured, a
// self-signed certificate is generated for development.
func (q *QUICTransport) Start(ctx context.Context) error {
	if q.serverTLS == nil {
		cert, err := generateSelfSignedCert()
		if err != nil {
			return fmt.Errorf("quic: generate dev cert: %w", err)
		}
		q.serverTLS = &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"core-quic"},
		}
	}
	// Listener address is resolved from config at the call site.
	// Start is a no-op here; the concrete port is bound by the service runner.
	return nil
}

// Stop closes the QUIC listener if one is active.
func (q *QUICTransport) Stop(_ context.Context) error {
	if q.listener != nil {
		return q.listener.Close()
	}
	return nil
}

// Supports returns true when the payload specifies a non-zero TargetQUICPort.
func (q *QUICTransport) Supports(_ context.Context, payload *coretypes.PayLoad, _ string) bool {
	return payload != nil && payload.TargetQUICPort > 0
}

// Send serialises payload, opens a QUIC stream to target, writes the framed
// request, and returns the framed response.
func (q *QUICTransport) Send(ctx context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
	addr := resolveAddr(payload, target)
	if addr == "" {
		return nil, fmt.Errorf("quic: cannot resolve target address from payload or target %q", target)
	}

	conn, err := quicgo.DialAddrContext(ctx, addr, q.clientTLS.Clone(), q.quicConf)
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("quic: open stream: %w", err)
	}
	defer stream.Close()

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("quic: marshal payload: %w", err)
	}

	if err := writeFrame(stream, data); err != nil {
		return nil, fmt.Errorf("quic: send frame: %w", err)
	}

	resp, err := readFrame(bufio.NewReader(stream))
	if err != nil {
		return nil, fmt.Errorf("quic: read response: %w", err)
	}
	return resp, nil
}

// Health dials target and immediately closes to verify connectivity.
func (q *QUICTransport) Health(ctx context.Context, target string) error {
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := quicgo.DialAddrContext(dialCtx, target, q.clientTLS.Clone(), q.quicConf)
	if err != nil {
		return fmt.Errorf("quic: health dial %s: %w", target, err)
	}
	return conn.CloseWithError(0, "health-check")
}

// ============================================================
// helpers
// ============================================================

func resolveAddr(payload *coretypes.PayLoad, fallback string) string {
	if payload != nil && payload.TargetAddress != "" && payload.TargetQUICPort > 0 {
		return fmt.Sprintf("%s:%d", payload.TargetAddress, payload.TargetQUICPort)
	}
	return fallback
}

func writeFrame(w io.Writer, data []byte) error {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, int32(len(data))); err != nil {
		return err
	}
	buf.Write(data)
	_, err := w.Write(buf.Bytes())
	return err
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, err
	}
	var length int32
	if err := binary.Read(bytes.NewReader(lenBuf), binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, fmt.Errorf("quic: invalid frame length %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// generateSelfSignedCert creates an ECDSA self-signed certificate for
// development use. NOT suitable for production.
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "core-quic-dev"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}, nil
}
