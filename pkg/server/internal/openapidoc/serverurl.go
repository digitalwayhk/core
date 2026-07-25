package openapidoc

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const fallbackHost = "127.0.0.1"

func serviceServerURL(req *http.Request, port int) string {
	scheme := requestScheme(req)
	host := requestHostName(req)
	if scheme == "https" {
		return validAbsoluteURL("https://" + formatHost(host) + "/")
	}
	return validAbsoluteURL("http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/")
}

func sameOriginServerURL(req *http.Request) string {
	return validAbsoluteURL(requestScheme(req) + "://" + requestAuthority(req) + "/")
}

func requestScheme(req *http.Request) string {
	if req != nil && req.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}

func requestHostName(req *http.Request) string {
	if req == nil {
		return fallbackHost
	}
	host := extractHost(req.Host)
	if !safeHost(host) {
		return fallbackHost
	}
	return host
}

func requestAuthority(req *http.Request) string {
	if req == nil {
		return fallbackHost
	}
	raw := strings.TrimSpace(req.Host)
	if raw == "" {
		return fallbackHost
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		host = strings.TrimSpace(host)
		if !safeHost(host) || !safePort(port) {
			return fallbackHost
		}
		return net.JoinHostPort(host, port)
	}
	host := extractHost(raw)
	if !safeHost(host) {
		return fallbackHost
	}
	return formatHost(host)
}

func extractHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" {
		return ""
	}
	if value, _, err := net.SplitHostPort(host); err == nil {
		return strings.TrimSpace(value)
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && len(host) > 2 {
		value := host[1 : len(host)-1]
		if net.ParseIP(value) != nil {
			return value
		}
		return ""
	}
	return host
}

func safeHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for index, char := range label {
			if char > unicode.MaxASCII {
				return false
			}
			valid := char >= 'a' && char <= 'z' ||
				char >= 'A' && char <= 'Z' ||
				char >= '0' && char <= '9' ||
				char == '-'
			if !valid || char == '-' && (index == 0 || index == len(label)-1) {
				return false
			}
		}
	}
	return true
}

func safePort(port string) bool {
	if port == "" {
		return false
	}
	for _, char := range port {
		if char < '0' || char > '9' {
			return false
		}
	}
	value, err := strconv.Atoi(port)
	return err == nil && value >= 1 && value <= 65535
}

func formatHost(host string) string {
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "[" + host + "]"
	}
	return host
}

func validAbsoluteURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return raw
	}
	scheme := "http"
	if strings.HasPrefix(raw, "https://") {
		scheme = "https"
	}
	return scheme + "://" + fallbackHost + "/"
}
