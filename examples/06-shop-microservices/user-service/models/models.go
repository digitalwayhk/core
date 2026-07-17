// Package models 保存 User Service 独占的用户和地址事实。
package models

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	actionOnce     sync.Once
	storageOnce    sync.Once
	action         persistencetypes.IDataAction
	actionTemplate persistencetypes.IDataAction
	storageErr     error
)

func baseAction() persistencetypes.IDataAction {
	actionOnce.Do(func() { action = entity.GetGlobalSqliteInstance(common.DatabaseName) })
	return action
}
func dataAction() persistencetypes.IDataAction {
	_ = EnsureStorage()
	if actionTemplate == nil {
		return baseAction()
	}
	if cloner, ok := actionTemplate.(interface {
		Clone() persistencetypes.IDataAction
	}); ok {
		return cloner.Clone()
	}
	return actionTemplate
}
func search(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}
func ensureWith(a persistencetypes.IDataAction, model interface{}) error {
	t := reflect.TypeOf(model)
	if t == nil || t.Kind() != reflect.Ptr {
		return errors.New("模型类型无效")
	}
	return a.Load(search(model, 1), reflect.New(reflect.SliceOf(t)).Interface())
}
func ensure(model interface{}) error { return ensureWith(dataAction(), model) }
func EnsureStorage() error {
	storageOnce.Do(func() {
		action := baseAction()
		for _, m := range []interface{}{NewUser(), NewAddress(), NewInbox()} {
			if err := ensureWith(action, m); err != nil {
				storageErr = err
				return
			}
		}
		if cloner, ok := action.(interface {
			Clone() persistencetypes.IDataAction
		}); ok {
			actionTemplate = cloner.Clone()
		} else {
			actionTemplate = action
		}
	})
	return storageErr
}

func EnsureUser(userID, name string) (*User, error) {
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	if userID == "" {
		return nil, errors.New("用户身份无效")
	}
	if err := ensure(NewUser()); err != nil {
		return nil, err
	}
	var items []*User
	q := search(NewUser(), 1)
	q.AddWhereN("AuthUserID", userID)
	if err := dataAction().Load(q, &items); err != nil {
		return nil, err
	}
	if len(items) > 0 {
		item := items[0]
		if name != "" && item.Name != name {
			item.Name = name
			item.SetUpdatedAt(time.Now().UTC())
			return item, dataAction().Update(item)
		}
		return item, nil
	}
	if name == "" {
		name = userID
	}
	item := NewUser()
	item.AuthUserID, item.Name, item.Enabled = userID, name, true
	item.SetHashcode(item.GetHash())
	return item, dataAction().Insert(item)
}

func FindUser(authUserID string) (*User, error) {
	if err := ensure(NewUser()); err != nil {
		return nil, err
	}
	var items []*User
	query := search(NewUser(), 1)
	query.AddWhereN("AuthUserID", strings.TrimSpace(authUserID))
	if err := dataAction().Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindUserByID(id uint) (*User, error) {
	if err := ensure(NewUser()); err != nil {
		return nil, err
	}
	var items []*User
	query := search(NewUser(), 1)
	query.AddWhereN("ID", id)
	if err := dataAction().Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func SaveUser(item *User) error {
	item.AuthUserID, item.Name = strings.TrimSpace(item.AuthUserID), strings.TrimSpace(item.Name)
	if item.AuthUserID == "" || item.Name == "" {
		return errors.New("用户身份和名称不能为空")
	}
	item.SetHashcode(item.GetHash())
	if item.CreatedAt == nil {
		return dataAction().Insert(item)
	}
	item.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(item)
}

func InsertAddress(item *Address) error {
	item.Recipient = strings.TrimSpace(item.Recipient)
	if item.UserID == 0 || item.Recipient == "" {
		return errors.New("用户和收件人不能为空")
	}
	item.SetHashcode(item.GetHash())
	return dataAction().Insert(item)
}
func FindOwnedAddress(userID uint, id uint) (*Address, error) {
	if err := ensure(NewAddress()); err != nil {
		return nil, err
	}
	var items []*Address
	q := search(NewAddress(), 1)
	q.AddWhereN("ID", id)
	q.AddWhereN("UserID", userID)
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}
func FindAddress(id uint) (*Address, error) {
	if err := ensure(NewAddress()); err != nil {
		return nil, err
	}
	var items []*Address
	query := search(NewAddress(), 1)
	query.AddWhereN("ID", id)
	if err := dataAction().Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}
func ListAddresses(userID uint) ([]*Address, error) {
	if err := ensure(NewAddress()); err != nil {
		return nil, err
	}
	var items []*Address
	q := search(NewAddress(), 100)
	q.AddWhereN("UserID", userID)
	err := dataAction().Load(q, &items)
	return items, err
}
func DeleteAddress(item *Address) error { return dataAction().Delete(item) }
func AddressDTO(item *Address) *userdto.Address {
	if item == nil {
		return nil
	}
	return &userdto.Address{ID: item.ID, Recipient: item.Recipient, Phone: item.Phone, Region: item.Region, Detail: item.Detail}
}
func AddressSnapshot(item *Address) userdto.AddressSnapshot {
	return userdto.AddressSnapshot{AddressID: item.ID, Recipient: item.Recipient, Phone: item.Phone, Region: item.Region, Detail: item.Detail}
}

var inboxMu sync.Mutex

func ProcessInbox(eventID, eventType string, operation func() error) error {
	inboxMu.Lock()
	defer inboxMu.Unlock()
	if err := ensure(NewInbox()); err != nil {
		return err
	}
	var items []*Inbox
	q := search(NewInbox(), 1)
	q.AddWhereN("EventID", eventID)
	if err := dataAction().Load(q, &items); err != nil {
		return err
	}
	if len(items) > 0 {
		if items[0].Processed {
			return nil
		}
		if err := operation(); err != nil {
			return err
		}
		items[0].Processed = true
		items[0].SetUpdatedAt(time.Now().UTC())
		return dataAction().Update(items[0])
	}
	item := NewInbox()
	item.EventID, item.EventType = eventID, eventType
	item.SetHashcode(item.GetHash())
	if err := dataAction().Insert(item); err != nil {
		return err
	}
	if err := operation(); err != nil {
		return err
	}
	item.Processed = true
	item.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(item)
}
