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

func (h *HTTPTransport) Send(ctx context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
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
	var maxResponseBytes int64
	if limited, ok := payload.Instance.(coretypes.IRouterResponseSizeLimit); ok {
		maxResponseBytes = limited.MaxResponseBytes()
	}
	return postJSON(ctx, h.client, target, data, payload.Token, payload.TraceID, maxResponseBytes)
}

func (h *HTTPTransport) Health(ctx context.Context, target string) error {
	if !strings.Contains(target, "://") {
		target = "http://" + target
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func postJSON(ctx context.Context, client *http.Client, url string, data []byte, token, traceID string, maxResponseBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
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
		var body []byte
		if maxResponseBytes > 0 {
			body, _ = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
			if int64(len(body)) > maxResponseBytes {
				return nil, fmt.Errorf("http: response exceeds %d bytes", maxResponseBytes)
			}
		} else {
			body, _ = io.ReadAll(resp.Body)
		}
		return nil, fmt.Errorf("http: status %d: %s", resp.StatusCode, body)
	}
	if maxResponseBytes <= 0 {
		return io.ReadAll(resp.Body)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, fmt.Errorf("http: response exceeds %d bytes", maxResponseBytes)
	}
	return body, nil
}
