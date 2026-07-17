package manage

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func adminOnly(req servertypes.IRequest) error {
	uid, _ := req.GetUser()
	if uid != contract.PlatformAdminUserID {
		return contract.ErrForbidden
	}
	return nil
}

func adminSearch(req servertypes.IRequest) (interface{}, error, bool) {
	if err := adminOnly(req); err != nil {
		return nil, err, true
	}
	return nil, nil, false
}
