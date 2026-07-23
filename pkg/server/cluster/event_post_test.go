package cluster

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCrossNodeSenderReceivesCompleteIPv6NodeInfo(t *testing.T) {
	var target *NodeInfo
	broker := &CrossNodeNoticeBroker{
		sender: func(_ context.Context, got *NodeInfo, _ []byte, _ string) ([]byte, error) {
			target = got
			return nil, nil
		},
	}

	if err := broker.post(&NodeInfo{ID: "node-v6", Address: "2001:db8::1", Port: 8080}, "/notice", struct{}{}); err != nil {
		t.Fatalf("sender 返回成功时 post 不应失败: %v", err)
	}
	if target == nil || target.Address != "2001:db8::1" || target.Port != 8080 {
		t.Fatalf("sender 未收到完整 NodeInfo: %#v", target)
	}
}

func TestCrossNodeSenderErrorDoesNotFallbackToHTTP(t *testing.T) {
	var httpCalls atomic.Int32
	broker := &CrossNodeNoticeBroker{
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			httpCalls.Add(1)
			return nil, errors.New("HTTP 不应被调用")
		})},
	}
	broker.SetSender(func(context.Context, *NodeInfo, []byte, string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	})

	err := broker.post(&NodeInfo{
		ID: "peer", Address: "127.0.0.1", Port: 8080, GRPCPort: 19090,
	}, "/api/servermanage/ws/notice", map[string]string{"id": "1"})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, httpCalls.Load())
}
