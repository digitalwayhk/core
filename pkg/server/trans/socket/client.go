package socket

import (
	"bufio"
	"fmt"
	"net"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type Client struct {
	Server
	conn net.Conn
}

func echo(conn *net.TCPConn) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		n, err := conn.Write([]byte(now.String()))
		if err != nil {
			logx.Errorw("socket_heartbeat_failed", logx.Field("error", err))
			_ = conn.Close()
			return
		}
		logx.Debugw("socket_heartbeat_sent", logx.Field("bytes", n))
	}
}

func (own *Client) Connect() error {
	conn, err := net.Dial("tcp", net.JoinHostPort(own.IP, fmt.Sprint(own.Port)))
	if err != nil {
		return fmt.Errorf("conn server %s:%d failed, err:%v", own.IP, own.Port, err)
	}
	own.conn = conn
	return nil
}

func (own *Client) Send(msg []byte) ([]byte, error) {
	mbyte, err := EncodeBytes(msg)
	if err != nil {
		return nil, fmt.Errorf("encode data failed, err:%v", err)
	}
	_, err = own.conn.Write(mbyte)
	if err != nil {
		return nil, fmt.Errorf("send data failed, err:%v", err)
	}
	reader := bufio.NewReader(own.conn)
	recv, err := DecodeBytes(reader)
	if err != nil {
		logx.Errorf("client read from conn failed, err:%v", err)
	}
	return recv, nil
}
func (own *Client) Close() {
	if own.conn != nil {
		own.conn.Close()
	}
}
