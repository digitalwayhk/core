package contract

import "errors"

var (
	ErrInvalidIdentity    = errors.New("用户身份无效")
	ErrForbidden          = errors.New("无权操作该资源")
	ErrResourceNotFound   = errors.New("资源不存在或无权访问")
	ErrServiceUnavailable = errors.New("目标服务暂不可用")
)
