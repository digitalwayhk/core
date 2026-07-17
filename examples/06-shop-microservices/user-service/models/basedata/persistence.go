package basedata

import (
	"errors"
	"strings"
	"time"

	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/internal/store"
)

func EnsureUser(userID, name string) (*User, error) {
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	if userID == "" {
		return nil, errors.New("用户身份无效")
	}
	if err := store.EnsureModel(NewUser()); err != nil {
		return nil, err
	}
	var items []*User
	q := store.NewSearch(NewUser(), 1)
	q.AddWhereN("AuthUserID", userID)
	if err := store.Get().Load(q, &items); err != nil {
		return nil, err
	}
	if len(items) > 0 {
		item := items[0]
		if name != "" && item.Name != name {
			item.Name = name
			item.SetUpdatedAt(time.Now().UTC())
			return item, store.Get().Update(item)
		}
		return item, nil
	}
	if name == "" {
		name = userID
	}
	item := NewUser()
	item.AuthUserID, item.Name, item.Enabled = userID, name, true
	item.SetHashcode(item.GetHash())
	return item, store.Get().Insert(item)
}

func FindUser(authUserID string) (*User, error) {
	if err := store.EnsureModel(NewUser()); err != nil {
		return nil, err
	}
	var items []*User
	query := store.NewSearch(NewUser(), 1)
	query.AddWhereN("AuthUserID", strings.TrimSpace(authUserID))
	if err := store.Get().Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindUserByID(id uint) (*User, error) {
	if err := store.EnsureModel(NewUser()); err != nil {
		return nil, err
	}
	var items []*User
	query := store.NewSearch(NewUser(), 1)
	query.AddWhereN("ID", id)
	if err := store.Get().Load(query, &items); err != nil || len(items) == 0 {
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
		return store.Get().Insert(item)
	}
	item.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(item)
}

func InsertAddress(item *Address) error {
	item.Recipient = strings.TrimSpace(item.Recipient)
	if item.UserID == 0 || item.Recipient == "" {
		return errors.New("用户和收件人不能为空")
	}
	item.SetHashcode(item.GetHash())
	return store.Get().Insert(item)
}

func FindOwnedAddress(userID uint, id uint) (*Address, error) {
	if err := store.EnsureModel(NewAddress()); err != nil {
		return nil, err
	}
	var items []*Address
	q := store.NewSearch(NewAddress(), 1)
	q.AddWhereN("ID", id)
	q.AddWhereN("UserID", userID)
	if err := store.Get().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindAddress(id uint) (*Address, error) {
	if err := store.EnsureModel(NewAddress()); err != nil {
		return nil, err
	}
	var items []*Address
	query := store.NewSearch(NewAddress(), 1)
	query.AddWhereN("ID", id)
	if err := store.Get().Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func ListAddresses(userID uint) ([]*Address, error) {
	if err := store.EnsureModel(NewAddress()); err != nil {
		return nil, err
	}
	var items []*Address
	q := store.NewSearch(NewAddress(), 100)
	q.AddWhereN("UserID", userID)
	err := store.Get().Load(q, &items)
	return items, err
}

func DeleteAddress(item *Address) error { return store.Get().Delete(item) }

func AddressDTO(item *Address) *userdto.Address {
	if item == nil {
		return nil
	}
	return &userdto.Address{ID: item.ID, Recipient: item.Recipient, Phone: item.Phone, Region: item.Region, Detail: item.Detail}
}

func AddressSnapshot(item *Address) userdto.AddressSnapshot {
	return userdto.AddressSnapshot{AddressID: item.ID, Recipient: item.Recipient, Phone: item.Phone, Region: item.Region, Detail: item.Detail}
}
