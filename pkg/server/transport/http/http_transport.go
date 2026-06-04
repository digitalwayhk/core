package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
)

// HTTPTransport implements transport.Transport over plain HTTP.
type HTTPTransport struct {
	client *http.Client
}

func New() *HTTPTransport {
	return &HTTPTransport{client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *HTTPTransport) Name() string                  { return "http" }
func (h *HTTPTransport) Start(_ context.Context) error { return nil }
func (h *HTTPTransport) Stop(_ context.Context) error  { return nil }

func (h *HTTPTransport) Supports(_ context.Context, _ *coretypes.PayLoad, _ string) bool {
	return true
}

func (h *HTTPTransport) Send(_ context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
	if !strings.Contains(target, "://") {
		target = "http://" + target
	}
	if payload.TargetPath != "" && !strings.HasSuffix(target, payload.TargetPath) {
		target = strings.TrimRight(target, "/") + "/" + strings.TrimLeft(payload.TargetPath, "/")
	}
	data, err := json.Marshal(payload.Instance)
	if err != nil {
		return nil, err
	}
	return postJSON(h.client, target, data, payload.Token, payload.TraceID)
}

func (h *HTTPTransport) Health(_ context.Context, target string) error {
	if !strings.Contains(target, "://") {
		target = "http://" + target
	}
	resp, err := h.client.Get(target)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func postJSON(client *http.Client, url string, data []byte, token, traceID string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if traceID != "" {
		req.Header.Set("X-Trace-Id", traceID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http: status %d: %s", resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}
