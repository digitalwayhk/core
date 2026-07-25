package melody

import (
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/olahol/melody"
	"github.com/zeromicro/go-zero/core/logx"
)

// MelodyClient 适配器，实现原有的Client接口
type MelodyClient struct {
	session *melody.Session
	manager *MelodyManager
}

func (mc *MelodyClient) Send(hash, path string, message interface{}) {
	// 🔧 添加：检查连接状态
	if mc.session.IsClosed() {
		logx.Errorf("尝试向已关闭的WebSocket连接发送消息: path=%s", path)
		return
	}

	mc.manager.sendToSession(mc.session, hash, path, message)
}

func (mc *MelodyClient) SendError(path string, err string) {
	// 🔧 添加：检查连接状态
	if mc.session.IsClosed() {
		logx.Errorf("尝试向已关闭的WebSocket连接发送错误: path=%s", path)
		return
	}
	mc.manager.sendError(mc.session, path, err)
}

func (mc *MelodyClient) IsClosed() bool {
	return mc.session.IsClosed()
}

// Close 实现可选 IWebSocketCloser，使身份撤销可以主动断开外部连接。
func (mc *MelodyClient) Close() error {
	if mc == nil || mc.session == nil || mc.session.IsClosed() {
		return nil
	}
	return mc.session.Close()
}

func (mc *MelodyClient) GetSubscriptions() map[string]map[int]types.IRouter {
	return mc.manager.GetSessionSubscriptions(mc.session)
}

func (mc *MelodyClient) GetChannelArgs(channel string) map[int]types.IRouter {
	return mc.manager.GetChannelSubscriptions(mc.session, channel)
}
