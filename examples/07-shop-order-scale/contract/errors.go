// Package contract 定义 07 订单水平扩展示例跨服务共享的错误契约。
package contract

import "errors"

var (
	// ErrInvalidIdentity 表示请求身份无效或无法映射到业务用户。
	ErrInvalidIdentity = errors.New("用户身份无效")

	// ErrForbidden 表示当前身份无权操作目标资源。
	ErrForbidden = errors.New("无权操作该资源")

	// ErrResourceNotFound 表示资源不存在或当前身份不可见。
	ErrResourceNotFound = errors.New("资源不存在或无权访问")

	// ErrServiceUnavailable 表示内部依赖服务暂不可用。
	ErrServiceUnavailable = errors.New("目标服务暂不可用")

	// ErrSubjectDisabled 表示供应商、商品或用户已禁用。
	ErrSubjectDisabled = errors.New("主体已禁用，只允许查看")

	// ErrResourceInUse 表示资源已被业务事实引用，不能物理删除。
	ErrResourceInUse = errors.New("资源已被使用，只能禁用")

	// ErrIdempotencyKeyReused 表示同一幂等键被用于不同请求指纹。
	ErrIdempotencyKeyReused = errors.New("幂等键已用于不同请求")

	// ErrInternalOnly 表示接口仅允许可信内部服务调用。
	ErrInternalOnly = errors.New("接口仅允许内部服务调用")

	// ErrOrderRuleViolated 表示订单请求不满足当前订单规则。
	ErrOrderRuleViolated = errors.New("订单不满足当前规则")
)
