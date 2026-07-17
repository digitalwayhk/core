package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

type actor struct {
	admin bool
	user  *models.User
}

func actorFrom(req servertypes.IRequest) (actor, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == contract.PlatformAdminUserID {
		return actor{admin: true}, nil
	}
	if uid == "" {
		return actor{}, contract.ErrInvalidIdentity
	}
	user, err := models.FindUser(uid)
	if err != nil || user == nil {
		return actor{}, contract.ErrResourceNotFound
	}
	return actor{user: user}, nil
}

func ownerSearch(item *view.SearchItem, req servertypes.IRequest, column string) (interface{}, error, bool) {
	actor, err := actorFrom(req)
	if err != nil {
		return nil, err, true
	}
	if actor.admin {
		return nil, nil, false
	}
	if item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item.WhereList = append(item.WhereList, &view.SearchWhere{Name: column, Value: actor.user.ID})
	return nil, nil, false
}

func authorizeWrite(actor actor, userID uint) error {
	if actor.admin {
		return nil
	}
	if actor.user == nil || actor.user.ID != userID {
		return contract.ErrForbidden
	}
	if !actor.user.Enabled {
		return contract.ErrSubjectDisabled
	}
	return nil
}
