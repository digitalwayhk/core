package router

import "github.com/digitalwayhk/core/pkg/server/types"

type trustedInternalRequest struct {
	types.IRequest
	caller string
}

func (r *trustedInternalRequest) TrustedInternalCaller() (string, bool) {
	return r.caller, r.caller != ""
}

func requestWithTrustedInternalCaller(req types.IRequest, caller string) types.IRequest {
	if req == nil || caller == "" {
		return req
	}
	return &trustedInternalRequest{IRequest: req, caller: caller}
}
