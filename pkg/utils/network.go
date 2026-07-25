// 本文件提供本机 IP、可信代理客户端地址和端口探测能力。
package utils

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// GetOutBoundIP 通过 UDP 路由选择取得默认出口 IP。
func GetOutBoundIP() (ip string, err error) {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	ip = strings.Split(localAddr.String(), ":")[0]
	return
}

// GetLocalIP 返回枚举到的第一个非回环 IPv4 地址。
func GetLocalIP() string {
	ips, _ := GetLocalIPs()
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// GetLocalIPs 返回所有非回环 IPv4 地址。
func GetLocalIPs() ([]string, error) {
	addr, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, address := range addr {
		ipnet, isvail := address.(*net.IPNet)
		if isvail && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips, nil
}

// HasLocalIPAddr 检测 IP 地址是否是内网地址
func HasLocalIPAddr(ip string) bool {
	return HasLocalIP(net.ParseIP(ip))
}

// HasLocalIP 检测 IP 地址是否是内网地址
// 通过直接对比ip段范围效率更高
func HasLocalIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}

	return ip4[0] == 10 || // 10.0.0.0/8
		(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) || // 172.16.0.0/12
		(ip4[0] == 169 && ip4[1] == 254) || // 169.254.0.0/16
		(ip4[0] == 192 && ip4[1] == 168) // 192.168.0.0/16
}

// ClientPublicIP 按可信代理链解析请求的客户端 IP。
func ClientPublicIP(r *http.Request, trustedProxies ...string) string {
	if r == nil {
		return ""
	}
	direct, ok := remoteIP(r.RemoteAddr)
	if !ok {
		return ""
	}
	trusted := proxyPrefixes(trustedProxies)
	if len(trusted) == 0 {
		if isLocalPeer(direct) && hasForwardingHeaders(r) {
			return ""
		}
		return direct.String()
	}
	if !containsIP(trusted, direct) {
		return direct.String()
	}

	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(forwarded[i]))
		if err != nil {
			continue
		}
		candidate = candidate.Unmap()
		if !isUnsafeForwardedClient(candidate) && !containsIP(trusted, candidate) {
			return candidate.String()
		}
	}

	if candidate, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		candidate = candidate.Unmap()
		if !isUnsafeForwardedClient(candidate) && !containsIP(trusted, candidate) {
			return candidate.String()
		}
	}
	return ""
}

func hasForwardingHeaders(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-Forwarded-For")) != "" ||
		strings.TrimSpace(r.Header.Get("X-Real-IP")) != ""
}

func isLocalPeer(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast()
}

func isUnsafeForwardedClient(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsUnspecified()
}

func remoteIP(remoteAddr string) (netip.Addr, bool) {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func proxyPrefixes(proxies []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(proxies))
	for _, proxy := range proxies {
		proxy = strings.TrimSpace(proxy)
		if prefix, err := netip.ParsePrefix(proxy); err == nil {
			prefixes = append(prefixes, prefix)
			continue
		}
		if addr, err := netip.ParseAddr(proxy); err == nil {
			addr = addr.Unmap()
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return prefixes
}

func containsIP(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ScanPort 在三秒超时内探测指定协议和端口能否建立连接。
func ScanPort(protocol string, hostname string, port int) bool {
	//fmt.Printf("scanning port %d \n", port)
	p := strconv.Itoa(port)
	addr := net.JoinHostPort(hostname, p)
	conn, err := net.DialTimeout(protocol, addr, 3*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}
