package utils

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientPublicIPIgnoresForwardingHeadersFromUntrustedPeer(t *testing.T) {
	req := ipRequest("198.51.100.10:4321", "203.0.113.5", "203.0.113.6")

	require.Equal(t, "198.51.100.10", ClientPublicIP(req, "10.0.0.0/8"))
}

func TestClientPublicIPAcceptsForwardingHeadersFromTrustedPeer(t *testing.T) {
	req := ipRequest("10.0.0.5:4321", "203.0.113.5", "")

	require.Equal(t, "203.0.113.5", ClientPublicIP(req, "10.0.0.0/8"))
}

func TestClientPublicIPWalksTrustedChainFromRight(t *testing.T) {
	req := ipRequest("10.0.0.5:4321", "192.0.2.123, 203.0.113.9, 10.0.0.4", "")

	require.Equal(t, "203.0.113.9", ClientPublicIP(req, "10.0.0.0/8"))
}

func TestClientPublicIPSkipsMalformedForwardedEntries(t *testing.T) {
	req := ipRequest("127.0.0.1:4321", "malformed, 203.0.113.7", "")

	require.Equal(t, "203.0.113.7", ClientPublicIP(req, "127.0.0.1"))
}

func TestClientPublicIPUsesRealIPFromTrustedPeer(t *testing.T) {
	req := ipRequest("127.0.0.1:4321", "", "203.0.113.8")

	require.Equal(t, "203.0.113.8", ClientPublicIP(req, "127.0.0.1/32"))
}

func TestClientPublicIPReturnsDirectIPv6(t *testing.T) {
	req := ipRequest("[2001:db8::1]:4321", "203.0.113.5", "")

	require.Equal(t, "2001:db8::1", ClientPublicIP(req))
}

func TestClientPublicIPRejectsForwardingHeadersFromUnconfiguredLocalPeer(t *testing.T) {
	req := ipRequest("127.0.0.1:4321", "203.0.113.5", "")

	require.Empty(t, ClientPublicIP(req))
}

func TestClientPublicIPKeepsDirectLoopbackWithoutForwardingHeaders(t *testing.T) {
	req := ipRequest("127.0.0.1:4321", "", "")

	require.Equal(t, "127.0.0.1", ClientPublicIP(req))
}

func TestClientPublicIPRejectsUnsafeCandidateFromTrustedProxy(t *testing.T) {
	req := ipRequest("10.0.0.5:4321", "127.0.0.1", "")

	require.Empty(t, ClientPublicIP(req, "10.0.0.0/8"))
}

func TestClientPublicIPSkipsUnsafeCandidateInForwardedChain(t *testing.T) {
	req := ipRequest("10.0.0.5:4321", "203.0.113.7, 169.254.10.5", "")

	require.Equal(t, "203.0.113.7", ClientPublicIP(req, "10.0.0.0/8"))
}

func TestClientPublicIPAllowsPrivateClientFromTrustedProxy(t *testing.T) {
	req := ipRequest("10.0.0.5:4321", "192.168.20.7", "")

	require.Equal(t, "192.168.20.7", ClientPublicIP(req, "10.0.0.0/8"))
}

func ipRequest(remoteAddr, forwardedFor, realIP string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	if realIP != "" {
		req.Header.Set("X-Real-IP", realIP)
	}
	return req
}
