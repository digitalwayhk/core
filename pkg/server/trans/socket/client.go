package socket

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type Client struct {
	Server
	conn net.Conn
}

func echo(conn *net.TCPConn) {
	tick := time.Tick(5 * time.Second) // 五秒的心跳间隔
	for now := range tick {
		n, err := conn.Write([]byte(now.String()))
		if err != nil {
			log.Println(err)
			conn.Close()
			return
		}
		fmt.Printf("send %d bytes to %s\n", n, conn.RemoteAddr())
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
