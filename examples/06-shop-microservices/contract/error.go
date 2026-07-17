package contract

import "errors"

var (
	ErrInvalidIdentity      = errors.New("用户身份无效")
	ErrForbidden            = errors.New("无权操作该资源")
	ErrResourceNotFound     = errors.New("资源不存在或无权访问")
	ErrServiceUnavailable   = errors.New("目标服务暂不可用")
	ErrSubjectDisabled      = errors.New("主体已禁用，只允许查看")
	ErrResourceInUse        = errors.New("资源已被使用，只能禁用")
	ErrIdempotencyKeyReused = errors.New("幂等键已用于不同请求")
	ErrInternalOnly         = errors.New("接口仅允许内部服务调用")
)
