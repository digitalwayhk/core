package rest

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

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
	subchannel map[types.IRouter]string
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
		for api, channel := range c.subchannel {
			info := c.hub.serviceContext.Router.GetRouter(channel)
			info.UnRegisterWebSocketClient(api, c)
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
			c.send <- []byte("数据格式不正确,无法转换为Message类型:" + err.Error())
			continue
		}
		msg.Channel = strings.Trim(msg.Channel, " ")
		info := c.hub.serviceContext.Router.GetRouter(msg.Channel)
		if info == nil {
			c.send <- []byte("Channel中的路由无法找到:" + msg.Channel)
			continue
		}
		if msg.Event != string(Call) && msg.Event != string(Subscribe) && msg.Event != string(UnSubscribe) {
			c.send <- []byte("Event数据不正确,只支持 sub,unsub,call")
			continue // 如果出错,则直接返回
		}
		if msg.Event == string(Call) {
			nr, err := parse(info, msg.Data)
			if err != nil {
				c.send <- []byte("数据格式不正确,无法转换为Request类型:" + err.Error())
				continue // 如果出错,则直接返回
			}
			if cr, ok := c.res.(types.IRequestClear); ok {
				cr.ClearTraceId()
				cr.SetPath(msg.Channel)
			}
			res := info.ExecDo(nr, c.res)
			c.Send(msg.Event, msg.Channel, res)
		}
		if msg.Event == string(Subscribe) {
			api, err := parse(info, msg.Data)
			if err != nil {
				c.send <- []byte("订阅错误:" + err.Error())
				continue
			}
			err = api.Validation(c.res)
			if err != nil {
				c.send <- []byte("订阅错误:" + err.Error())
				continue
			}
			info.RegisterWebSocketClient(api, c, c.res)
			c.subchannel[api] = msg.Channel
		}
		if msg.Event == string(UnSubscribe) {
			api, err := parse(info, msg.Data)
			if err != nil {
				c.send <- []byte("退订错误:" + err.Error())
				continue // 如果出错,则直接返回
			}
			info.UnRegisterWebSocketClient(api, c)
		}
		//message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//c.hub.broadcast <- message
	}
}
func parse(info *types.RouterInfo, data interface{}) (types.IRouter, error) {
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
		subchannel: make(map[types.IRouter]string),
	}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
