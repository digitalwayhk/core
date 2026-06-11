package socket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
)

// SocketTransport implements transport.Transport over TCP sockets.
type SocketTransport struct{}

func New() *SocketTransport { return &SocketTransport{} }

func (s *SocketTransport) Name() string                  { return "socket" }
func (s *SocketTransport) Start(_ context.Context) error { return nil }
func (s *SocketTransport) Stop(_ context.Context) error  { return nil }

// Supports returns true when the payload specifies a TargetSocketPort.
func (s *SocketTransport) Supports(_ context.Context, payload *coretypes.PayLoad, _ string) bool {
	return payload != nil && payload.TargetSocketPort > 0
}

// Send marshals payload and sends it via the TCP socket protocol.
func (s *SocketTransport) Send(_ context.Context, payload *coretypes.PayLoad, _ string) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp",
		fmt.Sprintf("%s:%d", payload.TargetAddress, payload.TargetSocketPort),
		10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("socket: dial %s:%d: %w",
			payload.TargetAddress, payload.TargetSocketPort, err)
	}
	defer conn.Close()
	return sendRecv(conn, data)
}

// Health dials the socket port, sends a ping frame, and verifies the peer
// responds with a valid frame. This confirms the target is actually running
// the socket protocol, not just listening on a TCP port.
func (s *SocketTransport) Health(ctx context.Context, target string) error {
	host, port, err := parseHostPort(target)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return err
	}
	defer conn.Close()

	// Set a deadline for the ping roundtrip.
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}

	// Send a minimal ping frame.
	ping := []byte("ping")
	encoded, err := encodeFrame(ping)
	if err != nil {
		return fmt.Errorf("socket: health encode: %w", err)
	}
	if _, err := conn.Write(encoded); err != nil {
		return fmt.Errorf("socket: health write: %w", err)
	}

	// Read the response — any valid frame means the peer speaks the protocol.
	if _, err := decodeFrame(bufio.NewReader(conn)); err != nil {
		return fmt.Errorf("socket: health read: %w", err)
	}
	return nil
}

func sendRecv(conn net.Conn, msg []byte) ([]byte, error) {
	encoded, err := encodeFrame(msg)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(encoded); err != nil {
		return nil, fmt.Errorf("socket: write: %w", err)
	}
	return decodeFrame(bufio.NewReader(conn))
}

func encodeFrame(data []byte) ([]byte, error) {
	length := int32(len(data))
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, length); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeFrame(reader *bufio.Reader) ([]byte, error) {
	lenBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, lenBytes); err != nil {
		return nil, err
	}
	var length int32
	if err := binary.Read(bytes.NewReader(lenBytes), binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, fmt.Errorf("socket: invalid frame length %d", length)
	}
	pack := make([]byte, length)
	if _, err := io.ReadFull(reader, pack); err != nil {
		return nil, err
	}
	return pack, nil
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
