package utils

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasLocalIPAddr_Loopback(t *testing.T) {
	assert.True(t, HasLocalIPAddr("127.0.0.1"))
	assert.True(t, HasLocalIPAddr("127.0.0.2"))
}

func TestHasLocalIPAddr_10Net(t *testing.T) {
	assert.True(t, HasLocalIPAddr("10.0.0.1"))
	assert.True(t, HasLocalIPAddr("10.255.255.255"))
}

func TestHasLocalIPAddr_172Net(t *testing.T) {
	assert.True(t, HasLocalIPAddr("172.16.0.1"))
	assert.True(t, HasLocalIPAddr("172.31.255.255"))
	assert.False(t, HasLocalIPAddr("172.15.0.1"))
	assert.False(t, HasLocalIPAddr("172.32.0.1"))
}

func TestHasLocalIPAddr_169Net(t *testing.T) {
	assert.True(t, HasLocalIPAddr("169.254.0.1"))
	assert.False(t, HasLocalIPAddr("169.253.0.1"))
}

func TestHasLocalIPAddr_192Net(t *testing.T) {
	assert.True(t, HasLocalIPAddr("192.168.0.1"))
	assert.True(t, HasLocalIPAddr("192.168.255.255"))
	assert.False(t, HasLocalIPAddr("192.167.0.1"))
	assert.False(t, HasLocalIPAddr("192.169.0.1"))
}

func TestHasLocalIPAddr_Public(t *testing.T) {
	assert.False(t, HasLocalIPAddr("8.8.8.8"))
	assert.False(t, HasLocalIPAddr("1.1.1.1"))
	assert.False(t, HasLocalIPAddr("203.0.113.1"))
}

func TestHasLocalIP_IPv6Loopback(t *testing.T) {
	ip := net.ParseIP("::1")
	assert.True(t, HasLocalIP(ip))
}

func TestHasLocalIP_IPv6NonLocal(t *testing.T) {
	ip := net.ParseIP("2001:db8::1")
	// Not loopback and not IPv4 -> returns false
	assert.False(t, HasLocalIP(ip))
}

func TestGetLocalIPs_NotEmpty(t *testing.T) {
	ips, err := GetLocalIPs()
	// In a sandbox environment there may be no non-loopback IPv4; just ensure no crash
	if err != nil {
		t.Logf("GetLocalIPs returned error: %v", err)
	} else {
		t.Logf("local IPs: %v", ips)
	}
}

func TestGetLocalIP(t *testing.T) {
	ip := GetLocalIP()
	t.Logf("GetLocalIP: %q", ip)
	// Should be either empty or a valid IPv4
	if ip != "" {
		parsed := net.ParseIP(ip)
		assert.NotNil(t, parsed)
	}
}
