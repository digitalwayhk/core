// Package http provides a Transport implementation that wraps the existing
// trans/rest HTTP client. It is kept intentionally thin; all HTTP logic lives
// in trans/rest and is accessed here through the adapter pattern.
package http

import (
	"context"
	"encoding/json"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
)

// HTTPTransport implements transport.Transport over plain HTTP.
type HTTPTransport struct{}

// New returns a ready-to-use HTTPTransport.
func New() *HTTPTransport { return &HTTPTransport{} }

func (h *HTTPTransport) Name() string { return "http" }

func (h *HTTPTransport) Start(_ context.Context) error { return nil }

func (h *HTTPTransport) Stop(_ context.Context) error { return nil }

// Supports returns true for all targets — HTTP is the universal fallback.
func (h *HTTPTransport) Supports(_ context.Context, _ *coretypes.PayLoad, _ string) bool {
	return true
}

// Send serialises payload.Instance to JSON and POSTs it to the target path.
func (h *HTTPTransport) Send(_ context.Context, payload *coretypes.PayLoad, target string) ([]byte, error) {
	data, err := json.Marshal(payload.Instance)
	if err != nil {
		return nil, err
	}
	return rest.PostJson(target, data, payload)
}

// Health performs a lightweight HTTP GET to the target address.
func (h *HTTPTransport) Health(_ context.Context, target string) error {
	_, err := rest.HttpGet(target, nil)
	return err
}
