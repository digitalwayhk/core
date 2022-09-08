package socket

import (
	"bufio"
	"errors"
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
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", own.IP, own.Port))
	if err != nil {
		return errors.New(fmt.Sprintf("conn server %s:%d failed, err:%v\n", own.IP, own.Port, err))
	}
	own.conn = conn
	return nil
}

func (own *Client) Send(msg []byte) ([]byte, error) {
	mbyte, err := EncodeBytes(msg)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("encode data failed, err:%v\n", err))
	}
	_, err = own.conn.Write(mbyte)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("send data failed, err:%v\n", err))
	}
	reader := bufio.NewReader(own.conn)
	recv, err := DecodeBytes(reader)
	if err != nil {
		logx.Error("client read from conn failed, err:%v\n", err)
	}
	return recv, nil
}
func (own *Client) Close() {
	if own.conn != nil {
		own.conn.Close()
	}
}
