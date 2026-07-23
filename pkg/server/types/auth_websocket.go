package types

// CasdoorIdentityChangedEventType 是认证撤销 Manager 与 WebSocket Hub 共用的本地控制事件。
const CasdoorIdentityChangedEventType = "auth.casdoor.identity.changed"

// CasdoorAuthorityUnavailableEventType 表示共享撤销权威不可用，已有 Casdoor 会话必须关闭。
const CasdoorAuthorityUnavailableEventType = "auth.casdoor.authority.unavailable"

// IWebSocketCloser 是 WebSocket 客户端可选实现的关闭能力。
// 它保持 IWebSocket 原接口兼容；未实现时 Hub 仍会移除租约，但无法主动断开传输连接。
type IWebSocketCloser interface {
	Close() error
}

// WebSocketAuthIdentity 是已通过签名、用途和撤销校验的不可变会话身份快照。
// Hub 只保存这些身份字段，不保存 Token、Claims 或当前请求。
type WebSocketAuthIdentity struct {
	ServiceName     string
	AuthType        AuthType
	Provider        string
	ProviderSubject string
	UID             string
	Username        string
	Generation      uint64
}

// IWebSocketAuthRequest 由传输层的认证请求包装实现，为 Hub 提供可信身份。
// 业务 Router 和客户端 payload 不应实现或伪造此接口。
type IWebSocketAuthRequest interface {
	GetWebSocketAuthIdentity() (WebSocketAuthIdentity, bool)
}
