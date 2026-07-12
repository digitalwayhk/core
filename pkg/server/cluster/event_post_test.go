package cluster

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCrossNodePostUsesIPv6SafeAddressAndRejectsNon2xx(t *testing.T) {
	var requestedURL string
	broker := &CrossNodeNoticeBroker{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Status:     "502 Bad Gateway",
				Body:       io.NopCloser(strings.NewReader("upstream failed")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
	}

	err := broker.post(&NodeInfo{ID: "node-v6", Address: "2001:db8::1", Port: 8080}, "/notice", map[string]string{"value": "test"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("非 2xx 响应应返回含状态码的错误，实际为 %v", err)
	}
	if requestedURL != "http://[2001:db8::1]:8080/notice" {
		t.Fatalf("IPv6 URL 拼接错误: %s", requestedURL)
	}
}

func TestCrossNodeSenderReceivesJoinedIPv6Target(t *testing.T) {
	var target string
	broker := &CrossNodeNoticeBroker{
		sender: func(_ context.Context, got string, _ []byte, _ string) ([]byte, error) {
			target = got
			return nil, nil
		},
	}

	if err := broker.post(&NodeInfo{ID: "node-v6", Address: "2001:db8::1", Port: 8080}, "/notice", struct{}{}); err != nil {
		t.Fatalf("sender 返回成功时 post 不应失败: %v", err)
	}
	if target != "[2001:db8::1]:8080" {
		t.Fatalf("sender target 未使用 net.JoinHostPort: %s", target)
	}
}
