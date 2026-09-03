package router

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/types"
)

var errKeyedServiceCallerUnavailable = errors.New("router: keyed service caller unavailable")

type trustedInternalRequest struct {
	types.IRequest
	caller string
}

func (r *trustedInternalRequest) TrustedInternalCaller() (string, bool) {
	return r.caller, r.caller != ""
}

// CallServiceWithKey 保留被可信内部调用身份包装前的 keyed service 能力。
func (r *trustedInternalRequest) CallServiceWithKey(
	router types.IRouter,
	hashKey string,
	callback ...func(types.IResponse),
) (types.IResponse, error) {
	caller, ok := r.IRequest.(types.IRequestKeyedServiceCaller)
	if !ok {
		return nil, errKeyedServiceCallerUnavailable
	}
	return caller.CallServiceWithKey(router, hashKey, callback...)
}

func requestWithTrustedInternalCaller(req types.IRequest, caller string) types.IRequest {
	if req == nil || caller == "" {
		return req
	}
	return &trustedInternalRequest{IRequest: req, caller: caller}
}
