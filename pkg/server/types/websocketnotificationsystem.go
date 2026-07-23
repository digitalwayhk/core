package types

// noticeJob 只为保持旧方法签名的包内兼容而保留，不承载运行时状态。
type noticeJob struct{}

// WebSocketNotificationSystem 是旧通知池的无状态兼容壳。
//
// Deprecated: 框架不再创建或运行此通知池。WebSocket 通知由 ServiceContext
// 独占的 RouteWebSocketHub 处理，调用方应通过 RouterInfo 的兼容方法访问。
type WebSocketNotificationSystem struct{}

func (*WebSocketNotificationSystem) Start() {}

func (*WebSocketNotificationSystem) Submit(*noticeJob) bool { return false }

func (*WebSocketNotificationSystem) Shutdown() {}

func (*WebSocketNotificationSystem) GetStats() map[string]interface{} {
	return map[string]interface{}{}
}

func (*WebSocketNotificationSystem) ResetStats() {}

func (*WebSocketNotificationSystem) IsHealthy() bool { return false }
