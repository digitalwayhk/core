// 本文件提供当前服务 Manage API 的公共权限、限域和横切 Hook 能力。
package common

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// Actor 定义本文件能力使用的核心结构。
type Actor struct {
	Admin bool
	User  *models.User
}

// ActorFrom 执行本文件能力对应的业务操作。
func ActorFrom(req servertypes.IRequest) (Actor, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == contract.PlatformAdminUserID {
		return Actor{Admin: true}, nil
	}
	if uid == "" {
		return Actor{}, contract.ErrInvalidIdentity
	}
	user, err := models.FindUser(uid)
	if err != nil || user == nil {
		return Actor{}, contract.ErrResourceNotFound
	}
	return Actor{User: user}, nil
}

// OwnerSearch 执行本文件能力对应的业务操作。
func OwnerSearch(item *view.SearchItem, req servertypes.IRequest, column string) (interface{}, error, bool) {
	actor, err := ActorFrom(req)
	if err != nil {
		return nil, err, true
	}
	if actor.Admin {
		return nil, nil, false
	}
	if item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item.WhereList = append(item.WhereList, &view.SearchWhere{Name: column, Value: actor.User.ID})
	return nil, nil, false
}

// AuthorizeWrite 执行本文件能力对应的业务操作。
func AuthorizeWrite(actor Actor, userID uint) error {
	if actor.Admin {
		return nil
	}
	if actor.User == nil || actor.User.ID != userID {
		return contract.ErrForbidden
	}
	if !actor.User.Enabled {
		return contract.ErrSubjectDisabled
	}
	return nil
}
