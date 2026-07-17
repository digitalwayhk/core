package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

type manageActor struct {
	admin    bool
	supplier *models.Supplier
}

func actorFromRequest(req servertypes.IRequest) (manageActor, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == contract.PlatformAdminUserID {
		return manageActor{admin: true}, nil
	}
	if uid == "" {
		return manageActor{}, contract.ErrInvalidIdentity
	}
	supplier, err := models.FindSupplier(uid)
	if err != nil || supplier == nil {
		return manageActor{}, contract.ErrResourceNotFound
	}
	return manageActor{supplier: supplier}, nil
}

func addOwnerWhere(item *view.SearchItem, req servertypes.IRequest, column string) (interface{}, error, bool) {
	actor, err := actorFromRequest(req)
	if err != nil {
		return nil, err, true
	}
	if actor.admin {
		return nil, nil, false
	}
	if item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item.WhereList = append(item.WhereList, &view.SearchWhere{Name: column, Value: actor.supplier.ID})
	return nil, nil, false
}

func authorizeSupplierWrite(actor manageActor, supplier *models.Supplier) error {
	if supplier == nil {
		return contract.ErrResourceNotFound
	}
	if actor.admin {
		return nil
	}
	if actor.supplier == nil || actor.supplier.ID != supplier.ID {
		return contract.ErrForbidden
	}
	if !actor.supplier.Enabled {
		return contract.ErrSubjectDisabled
	}
	return nil
}
