// 本文件定义框架会话持有的语义引用；不保存认证token或业务消息。
package ownership

// Reference 是一个Gateway会话对scope/key资源的独立引用。
type Reference struct {
	GatewayID string
	SessionID string
	Name      string
}

// ReferenceSnapshot 是有界的完整引用集合；Revision来自Broker单调序列。
type ReferenceSnapshot struct {
	Revision   int64
	References []Reference
}
