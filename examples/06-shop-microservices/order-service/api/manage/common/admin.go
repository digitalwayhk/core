package common

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func AdminOnly(req servertypes.IRequest) error {
	uid, _ := req.GetUser()
	if uid != contract.PlatformAdminUserID {
		return contract.ErrForbidden
	}
	return nil
}

func AdminSearch(req servertypes.IRequest) (interface{}, error, bool) {
	if err := AdminOnly(req); err != nil {
		return nil, err, true
	}
	return nil, nil, false
}
