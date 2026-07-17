package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
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

type UserManage struct {
	*managepkg.ManageService[models.User]
	SetEnabled *SetUserEnabled
}

func NewUserManage() *UserManage {
	own := &UserManage{}
	own.ManageService = managepkg.NewManageService[models.User](own)
	own.SetEnabled = NewSetUserEnabled(own)
	return own
}
func (own *UserManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.SetEnabled}
}
func (*UserManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.User])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return ownerSearch(search.SearchItem, req, "ID")
}
func (own *UserManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := actorFrom(req)
	if err != nil {
		return nil, err, true
	}
	switch operation := sender.(type) {
	case *managepkg.Edit[models.User]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := authorizeWrite(actor, operation.OldItem.ID); err != nil {
			return nil, err, true
		}
		current := operation.OldItem
		current.Name = strings.TrimSpace(operation.Model.Name)
		return current, models.SaveUser(current), true
	case *SetUserEnabled:
		if !actor.admin {
			return nil, contract.ErrForbidden, true
		}
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, err := models.FindUserByID(operation.Model.ID)
		if err != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current.Enabled = operation.Model.Enabled
		return current, models.SaveUser(current), true
	}
	return nil, nil, false
}
func (*UserManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "用户管理", true
}
func (*UserManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("AuthUserID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}

type SetUserEnabled struct {
	managepkg.Operation[models.User]
}

func NewSetUserEnabled(owner interface{}) *SetUserEnabled {
	return &SetUserEnabled{Operation: managepkg.NewOperation[models.User](owner)}
}
func (own *SetUserEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetUserEnabled(nil)
	next.Operation.New(instance)
	return next
}
func (own *SetUserEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*UserManage).DoBefore(own, req)
	return result, err
}
func (own *SetUserEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }

type AddressManage struct {
	*managepkg.ManageService[models.Address]
}

func NewAddressManage() *AddressManage {
	own := &AddressManage{}
	own.ManageService = managepkg.NewManageService[models.Address](own)
	return own
}
func (own *AddressManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove}
}
func (*AddressManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.Address])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return ownerSearch(search.SearchItem, req, "UserID")
}
func (*AddressManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := actorFrom(req)
	if err != nil {
		return nil, err, true
	}
	switch operation := sender.(type) {
	case *managepkg.Add[models.Address]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if !actor.admin {
			operation.Model.UserID = actor.user.ID
		}
		if err := authorizeWrite(actor, operation.Model.UserID); err != nil {
			return nil, err, true
		}
	case *managepkg.Edit[models.Address]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := authorizeWrite(actor, operation.OldItem.UserID); err != nil {
			return nil, err, true
		}
		operation.Model.UserID = operation.OldItem.UserID
	case *managepkg.Remove[models.Address]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, err := models.FindAddress(operation.Model.ID)
		if err != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := authorizeWrite(actor, current.UserID); err != nil {
			return nil, err, true
		}
	}
	return nil, nil, false
}
func (*AddressManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "地址管理", true
}
func (*AddressManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("UserID") {
		field.IsEdit = false
	}
}
