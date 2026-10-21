// 本文件以可选接口增加持续归属调用，不破坏既有IRequest实现。
package types

import "context"

// IRequestOwnedServiceCaller 将内部调用权限与Ready owner精确选择组合。
type IRequestOwnedServiceCaller interface {
	CallServiceOwned(context.Context, IRouter, string, string) (IResponse, error)
}

// IWebSocketOwnedReference 由认证和参数校验后的WS路由声明语义引用，不返回认证凭据。
type IWebSocketOwnedReference interface {
	WebSocketOwnedReference() (scope, key, name string)
}
