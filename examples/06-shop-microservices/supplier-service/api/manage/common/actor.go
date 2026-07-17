package common

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

type Actor struct {
	Admin    bool
	Supplier *models.Supplier
}

func ActorFromRequest(req servertypes.IRequest) (Actor, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == contract.PlatformAdminUserID {
		return Actor{Admin: true}, nil
	}
	if uid == "" {
		return Actor{}, contract.ErrInvalidIdentity
	}
	supplier, err := models.FindSupplier(uid)
	if err != nil || supplier == nil {
		return Actor{}, contract.ErrResourceNotFound
	}
	return Actor{Supplier: supplier}, nil
}

func AddOwnerWhere(item *view.SearchItem, req servertypes.IRequest, column string) (interface{}, error, bool) {
	actor, err := ActorFromRequest(req)
	if err != nil {
		return nil, err, true
	}
	if actor.Admin {
		return nil, nil, false
	}
	if item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item.WhereList = append(item.WhereList, &view.SearchWhere{Name: column, Value: actor.Supplier.ID})
	return nil, nil, false
}

func AuthorizeSupplierWrite(actor Actor, supplier *models.Supplier) error {
	if supplier == nil {
		return contract.ErrResourceNotFound
	}
	if actor.Admin {
		return nil
	}
	if actor.Supplier == nil || actor.Supplier.ID != supplier.ID {
		return contract.ErrForbidden
	}
	if !actor.Supplier.Enabled {
		return contract.ErrSubjectDisabled
	}
	return nil
}
