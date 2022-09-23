package socket

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type Server struct {
	IP      string
	Port    int
	Name    string
	conns   int
	listen  net.Listener
	Receive func(data []byte) interface{}
	context *router.ServiceContext
	clients map[string]*Client
	sync.RWMutex
}

func (own *Server) process(conn net.Conn) {
	own.conns += 1
	// 处理完关闭连接
	defer func() {
		own.conns -= 1
		conn.Close()
	}()
	// 针对当前连接做发送和接受操作
	for {
		reader := bufio.NewReader(conn)
		// 读取客户端发送的数据
		recv, err := DecodeBytes(reader)
		data := own.Receive(recv)
		value, _ := json.Marshal(data)
		// 将接受到的数据返回给客户端
		wres, err := EncodeBytes(value)
		_, err = conn.Write(wres)
		if err != nil {
			fmt.Printf("write from conn failed, err:%v\n", err)
			break
		}
	}
}
func NewServer(context *router.ServiceContext) *Server {
	ser := &Server{
		context: context,
		IP:      context.Config.RunIp,
		Port:    context.Config.SocketPort,
		Name:    context.Config.Name,
		clients: make(map[string]*Client),
	}
	return ser
}
func (own *Server) Start() {
	if own.Port == 0 {
		return
	}
	time.Sleep(time.Millisecond)
	own.register()
	err := own.tcpLisen()
	if err != nil {
		panic(err)
	}
}
func (own *Server) Stop() {
	if own.listen != nil {
		err := own.listen.Close()
		own.listen = nil
		if err != nil {
			logx.Error(err)
		}
	}
	for _, c := range own.clients {
		c.Close()
	}
}

func (own *Server) tcpLisen() error {
	listen, err := net.Listen("tcp", fmt.Sprintf("%s:%d", own.IP, own.Port))
	if err != nil {
		return errors.New(fmt.Sprintf("listen failed, err:%v\n", err))
	} else {
		//fmt.Println("===========================================================")
		fmt.Printf("Starting %s socket listen %s:%d success\n", own.Name, own.IP, own.Port)
	}
	own.listen = listen
	for {
		if own.listen != nil {
			conn, err := own.listen.Accept()
			//logx.Info(fmt.Sprintf("accept conn from %s:%d", conn.RemoteAddr().String(), own.Port))
			if err != nil {
				logx.Error("accept failed, err:%v\n", err)
				continue
			}
			// 启动一个单独的 goroutine 去处理连接
			go own.process(conn)
		}
	}
}
func (own *Server) register() {
	if own.Receive == nil {
		own.Receive = func(data []byte) interface{} {
			payload := &types.PayLoad{}
			err := json.Unmarshal(data, payload)
			req := router.ToRequest(payload)
			if err != nil {
				// res := req.NewResponse(err, nil)
				// data, _ := json.Marshal(res)
				// payload.Data = data
				return req.NewResponse(err, nil)
			}
			rou := own.context.Router.GetRouter(payload.TargetPath)
			if rou != nil {
				api, err := rou.ParseNew(payload.Instance)
				if err != nil {
					return req.NewResponse(err, nil)
				}
				// data, err := json.Marshal()
				// payload.Data = data
				return rou.ExecDo(api, req)
			}
			// res := req.NewResponse(errors.New(payload.TargetPath+"未找到对应的接口！"), nil)
			// payload.Data, _ = json.Marshal(res)
			return req.NewResponse(errors.New(payload.TargetPath+"未找到对应的接口！"), nil)
		}
	}
}

func (own *Server) RegisterHandlers(routers []*types.RouterInfo) {

}
func (own *Server) Send(payload *types.PayLoad) ([]byte, error) {
	//defer own.Unlock()
	values, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if payload.TargetSocketPort > 0 {
		//own.Lock()
		if _, ok := own.clients[payload.TargetService]; !ok {
			cs := &Client{
				Server: Server{
					IP:   payload.TargetAddress,
					Port: payload.TargetSocketPort,
				},
			}
			err := cs.Connect()
			if err != nil {
				return nil, err
			}
			own.clients[payload.TargetService] = cs
		}
		client := own.clients[payload.TargetService]
		txt, err := client.Send(values)
		if err != nil {
			return nil, err
		}
		return txt, err
	}
	return nil, nil
}
func (own *Server) GetIPandPort() (string, int) {
	return own.IP, own.Port
}
