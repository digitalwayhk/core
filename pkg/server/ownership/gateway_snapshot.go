// 本文件定义由现有WS会话采样产生的有界完整Gateway视图，不创建第二套应用会话账。
package ownership

import "time"

// ScopedReference 将语义引用定位到应用声明的scope/key。
type ScopedReference struct {
	Scope     string
	Key       string
	SessionID string
	Name      string
}

// GatewaySnapshot 的Revision由单次Gateway启动会话单调产生，旧快照不得覆盖新退订。
type GatewaySnapshot struct {
	GatewayID  string
	Revision   int64
	References []ScopedReference
	Lease      time.Duration
}
