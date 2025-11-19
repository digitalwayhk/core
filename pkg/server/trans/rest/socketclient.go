package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512

	// send buffer size
	bufSize = 256
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub
	res types.IRequest
	// The websocket connection.
	conn *websocket.Conn
	// Buffered channel of outbound messages.
	send       chan []byte
	subchannel map[string]map[uint64]types.IRouter
	isClose    bool
}

func (own *Client) Send(hash, path string, message interface{}) {
	msg := &Message{
		Event:   hash,
		Channel: path,
		Data:    message,
	}
	dres, _ := json.Marshal(msg)
	own.send <- dres
}
func (own *Client) SendError(path string, err string) {
	msg := &Message{
		Event:   "error",
		Channel: path,
		Data:    err,
	}
	dres, _ := json.Marshal(msg)
	own.send <- dres
}

func (own *Client) IsClosed() bool {
	return own.isClose
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		for channel, items := range c.subchannel {
			info := c.hub.serviceContext.Router.GetRouter(channel)
			if info != nil {
				for hash := range items {
					info.UnRegisterWebSocketHash(hash, c)
				}
			}
		}
		c.hub.unregister <- c
		c.conn.Close()
		c.isClose = true
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		msg := &Message{}
		if err := json.Unmarshal(message, msg); err != nil {
			c.SendError("", "数据格式不正确,无法转换为Message类型:"+err.Error())
			continue
		}
		if msg.Event == string(Get) {
			if msg.Channel != "" {
				c.Send(msg.Event, msg.Channel, c.subchannel[msg.Channel])
				continue
			}
			c.Send(msg.Event, msg.Channel, c.subchannel)
			continue
		}
		msg.Channel = strings.Trim(msg.Channel, " ")
		info := c.hub.serviceContext.Router.GetRouter(msg.Channel)
		if info == nil {
			c.SendError(msg.Channel, "当前服务中未找到对应的路由")
			continue
		}
		if msg.Event != string(Call) && msg.Event != string(Subscribe) && msg.Event != string(UnSubscribe) {
			c.SendError(msg.Channel, "Event数据不正确,只支持 sub,unsub,call")
			continue // 如果出错,则直接返回
		}
		if cr, ok := c.res.(types.IRequestClear); ok {
			cr.ClearTraceId()
			cr.SetPath(msg.Channel)
		}
		if msg.Event == string(Call) {
			nr, err := parse(info, msg.Data)
			if err != nil {
				c.SendError(msg.Channel, "数据格式不正确,无法转换为Request类型:"+err.Error())
				continue // 如果出错,则直接返回
			}
			res := info.ExecDo(nr, c.res)
			c.Send(msg.Event, msg.Channel, res)
		}
		if msg.Event == string(Subscribe) {
			api, err := parse(info, msg.Data)
			if err != nil {
				c.SendError(msg.Channel, "订阅错误:"+err.Error())
				continue
			}
			err = api.Validation(c.res)
			if err != nil {
				c.SendError(msg.Channel, "订阅错误:"+err.Error())
				continue
			}
			hash := info.RegisterWebSocketClient(api, c, c.res)
			if _, ok := c.subchannel[msg.Channel]; !ok {
				c.subchannel[msg.Channel] = make(map[uint64]types.IRouter)
			}
			c.subchannel[msg.Channel][hash] = api
			c.Send(msg.Event, msg.Channel, c.subchannel)
		}
		if msg.Event == string(UnSubscribe) {
			hash, ok := msg.Data.(float64)
			if ok && hash > 0 {
				hint := uint64(hash)
				if hs, ok := c.subchannel[msg.Channel]; ok {
					if _, ok := hs[hint]; ok {
						info.UnRegisterWebSocketHash(hint, c)
						delete(hs, hint)
						continue
					}
				}
				hashStr := strconv.FormatUint(hint, 10)
				c.SendError(msg.Channel, "退订错误:"+msg.Channel+"未找到订阅"+hashStr)
				continue
			}
			api, err := parse(info, msg.Data)
			if err != nil {
				c.SendError(msg.Channel, "退订错误:"+err.Error())
				continue // 如果出错,则直接返回
			}
			api.Validation(c.res)
			hint := info.UnRegisterWebSocketClient(api, c)
			if hint > 0 {
				delete(c.subchannel[msg.Channel], hint)
			}
		}
		//message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//c.hub.broadcast <- message
	}
}
func parse(info *types.RouterInfo, data interface{}) (types.IRouter, error) {
	defer func() {
		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:ParseNew", info.ServiceName, info.Path), err)
		}
	}()
	var api types.IRouter
	var err error
	if data == nil {
		api = info.New()
	} else {
		api, err = info.ParseNew(data)
	}
	if err != nil {
		return nil, errors.New("数据格式不正确,无法转换为Request类型:" + err.Error())
	}
	return api, nil
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{
		hub:        hub,
		res:        router.NewRequest(hub.serviceContext.Router, r),
		conn:       conn,
		send:       make(chan []byte, bufSize),
		subchannel: make(map[string]map[uint64]types.IRouter),
	}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
