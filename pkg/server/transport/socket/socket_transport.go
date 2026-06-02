// Package socket provides a Transport implementation that wraps the existing
// trans/socket TCP client. Health is checked with a dial attempt.
package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/server/trans/socket"
)

// SocketTransport implements transport.Transport over TCP sockets.
type SocketTransport struct{}

// New returns a ready-to-use SocketTransport.
func New() *SocketTransport { return &SocketTransport{} }

func (s *SocketTransport) Name() string { return "socket" }

func (s *SocketTransport) Start(_ context.Context) error { return nil }

func (s *SocketTransport) Stop(_ context.Context) error { return nil }

// Supports returns true when the payload specifies a TargetSocketPort,
// meaning the target service has a socket listener.
func (s *SocketTransport) Supports(_ context.Context, payload *coretypes.PayLoad, _ string) bool {
	return payload != nil && payload.TargetSocketPort > 0
}

// Send marshals payload and sends it via the TCP socket client.
func (s *SocketTransport) Send(_ context.Context, payload *coretypes.PayLoad, _ string) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	c := &socket.Client{}
	c.IP = payload.TargetAddress
	c.Port = payload.TargetSocketPort
	if err := c.Connect(); err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Send(data)
}

// Health dials the socket port and closes immediately to verify reachability.
func (s *SocketTransport) Health(_ context.Context, target string) error {
	host, portStr, err := parseHostPort(target)
	if err != nil {
		return err
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, portStr))
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func parseHostPort(target string) (string, int, error) {
	if !strings.Contains(target, ":") {
		return target, 0, fmt.Errorf("socket: no port in target %q", target)
	}
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
