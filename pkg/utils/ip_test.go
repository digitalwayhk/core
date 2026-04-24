package utils

import (
	"net"
	"net/http"
	"testing"
)

func TestHasLocalIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":      true,  // loopback
		"10.0.0.1":       true,  // 10/8
		"10.255.255.255": true,
		"172.16.0.1":     true,  // 172.16/12
		"172.31.255.254": true,
		"172.32.0.1":     false, // outside 12-bit private range
		"172.15.0.1":     false,
		"192.168.1.100":  true,
		"169.254.0.1":    true,  // link-local
		"8.8.8.8":        false, // public
		"1.1.1.1":        false,
	}
	for ipStr, want := range cases {
		if got := HasLocalIPAddr(ipStr); got != want {
			t.Errorf("HasLocalIPAddr(%q) = %v, want %v", ipStr, got, want)
		}
	}
}

func TestHasLocalIP_NonIPv4(t *testing.T) {
	// IPv6 addresses other than loopback are reported as not local.
	v6 := net.ParseIP("2001:db8::1")
	if HasLocalIP(v6) {
		t.Errorf("public IPv6 should not be local")
	}
	// Loopback works for v6 as well via IsLoopback.
	if !HasLocalIP(net.ParseIP("::1")) {
		t.Errorf("::1 should be local")
	}
}

func TestClientPublicIP_XForwardedFor(t *testing.T) {
	r := &http.Request{Header: http.Header{}, RemoteAddr: "9.9.9.9:1234"}
	r.Header.Set("X-Forwarded-For", "  203.0.113.5 , 198.51.100.1 ")
	if got := ClientPublicIP(r); got != "203.0.113.5" {
		t.Errorf("ClientPublicIP X-Forwarded-For = %q, want 203.0.113.5", got)
	}
}

func TestClientPublicIP_XRealIP(t *testing.T) {
	r := &http.Request{Header: http.Header{}, RemoteAddr: "9.9.9.9:1234"}
	r.Header.Set("X-Real-Ip", "  198.51.100.7 ")
	if got := ClientPublicIP(r); got != "198.51.100.7" {
		t.Errorf("ClientPublicIP X-Real-Ip = %q, want 198.51.100.7", got)
	}
}

func TestClientPublicIP_RemoteAddr(t *testing.T) {
	r := &http.Request{Header: http.Header{}, RemoteAddr: "9.9.9.9:1234"}
	if got := ClientPublicIP(r); got != "9.9.9.9" {
		t.Errorf("ClientPublicIP RemoteAddr = %q, want 9.9.9.9", got)
	}
}

func TestClientPublicIP_Empty(t *testing.T) {
	// No headers, malformed RemoteAddr -> empty string.
	r := &http.Request{Header: http.Header{}, RemoteAddr: "not-an-addr"}
	if got := ClientPublicIP(r); got != "" {
		t.Errorf("ClientPublicIP empty case = %q, want \"\"", got)
	}
}

func TestGetLocalIPs(t *testing.T) {
	// Should not error on any reasonable host. We can't assert specific values
	// (containers may have no non-loopback IPv4 interfaces) but the call must
	// succeed and return a slice (possibly empty).
	ips, err := GetLocalIPs()
	if err != nil {
		t.Fatalf("GetLocalIPs error: %v", err)
	}
	for _, ip := range ips {
		if net.ParseIP(ip) == nil {
			t.Errorf("GetLocalIPs returned non-IP value: %q", ip)
		}
	}
	// GetLocalIP should be the first entry (or empty if none).
	first := GetLocalIP()
	if len(ips) > 0 {
		if first != ips[0] {
			t.Errorf("GetLocalIP = %q, want %q", first, ips[0])
		}
	} else if first != "" {
		t.Errorf("GetLocalIP = %q, want empty string", first)
	}
}

func TestScanPort_Closed(t *testing.T) {
	// Port 1 on localhost is essentially never listening; expect false. We use
	// a short timeout via a non-listening loopback port number that's unlikely
	// to be in use. The function uses a 3s timeout internally on failure.
	if ScanPort("tcp", "127.0.0.1", 1) {
		t.Errorf("expected ScanPort to return false for closed port")
	}
}

func TestScanPort_Open(t *testing.T) {
	// Open an ephemeral listener and scan it.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()
	port := lis.Addr().(*net.TCPAddr).Port
	if !ScanPort("tcp", "127.0.0.1", port) {
		t.Errorf("expected ScanPort to detect open port %d", port)
	}
}
