package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type GRPCTestIdentity struct {
	CertFile   string
	KeyFile    string
	ServerName string
}

type GRPCTestPKI struct {
	CAFile   string
	Services map[string]GRPCTestIdentity
	Client   GRPCTestIdentity
}

// NewGRPCTestPKI creates a test-only CA and identities without shelling out to
// openssl. Service certificates are valid for server and client mTLS use.
func NewGRPCTestPKI(t testing.TB, serviceNames ...string) *GRPCTestPKI {
	t.Helper()
	directory := t.TempDir()
	now := time.Now()
	caKey := newECDSAKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber:          serialNumber(t),
		Subject:               pkix.Name{CommonName: "core integration CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create integration CA: %v", err)
	}
	caFile := filepath.Join(directory, "ca.crt")
	writePEMFile(t, caFile, "CERTIFICATE", caDER, 0o644)

	pki := &GRPCTestPKI{CAFile: caFile, Services: make(map[string]GRPCTestIdentity, len(serviceNames))}
	for _, serviceName := range serviceNames {
		serviceName = strings.TrimSpace(serviceName)
		if serviceName == "" {
			t.Fatal("gRPC test PKI service name must not be empty")
		}
		pki.Services[serviceName] = issueIdentity(t, directory, serviceName, caTemplate, caKey, now, true)
	}
	pki.Client = issueIdentity(t, directory, "client", caTemplate, caKey, now, false)
	return pki
}

func issueIdentity(t testing.TB, directory, name string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time, server bool) GRPCTestIdentity {
	t.Helper()
	key := newECDSAKey(t)
	usage := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	dnsNames := []string{name, "localhost"}
	if server {
		usage = append(usage, x509.ExtKeyUsageServerAuth)
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber(t),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(12 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usage,
		DNSNames:     dnsNames,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create integration identity %q: %v", name, err)
	}
	base := strings.NewReplacer("/", "-", "\\", "-", "..", "-").Replace(name)
	certFile := filepath.Join(directory, base+".crt")
	keyFile := filepath.Join(directory, base+".key")
	writePEMFile(t, certFile, "CERTIFICATE", certificateDER, 0o644)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal integration identity %q key: %v", name, err)
	}
	writePEMFile(t, keyFile, "EC PRIVATE KEY", keyDER, 0o600)
	return GRPCTestIdentity{CertFile: certFile, KeyFile: keyFile, ServerName: name}
}

func newECDSAKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate integration key: %v", err)
	}
	return key
}

func serialNumber(t testing.TB) *big.Int {
	t.Helper()
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		t.Fatalf("generate certificate serial: %v", err)
	}
	return serial
}

func writePEMFile(t testing.TB, path, blockType string, contents []byte, mode os.FileMode) {
	t.Helper()
	encoded := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: contents})
	if encoded == nil {
		t.Fatalf("encode PEM %s", path)
	}
	if err := os.WriteFile(path, encoded, mode); err != nil {
		t.Fatalf("write PEM %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod PEM %s: %v", path, err)
	}
}
