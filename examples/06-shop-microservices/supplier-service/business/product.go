// Package business 组装 Supplier Service 的所有权、有效性与事务规则。
package business

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

func ProductListResponse() interface{} { return []*supplierdto.Product{} }

func ProductResponse(item *models.Product) *supplierdto.Product {
	if item == nil {
		return nil
	}
	supplier, _ := models.FindSupplier(item.SupplierID)
	name := ""
	if supplier != nil {
		name = supplier.Name
	}
	return &supplierdto.Product{ID: item.ID, SupplierID: item.SupplierID, SupplierName: name, Name: item.Name, Code: item.Code, Price: item.Price, Enabled: item.Enabled}
}

func EnsureSupplier(userID, name string) (*models.Supplier, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == contract.PlatformAdminUserID {
		return nil, contract.ErrInvalidIdentity
	}
	item, err := models.FindSupplier(userID)
	if err == nil && item != nil {
		if strings.TrimSpace(name) != "" && item.Name != strings.TrimSpace(name) {
			item.Name = strings.TrimSpace(name)
			err = item.Save()
		}
		return item, err
	}
	item = models.NewSupplier()
	item.UserID, item.Name, item.Code, item.Enabled = userID, strings.TrimSpace(name), "supplier-"+strings.ToLower(userID), true
	if item.Name == "" {
		item.Name = userID
	}
	return item, item.Save()
}

func CreateProduct(ownerID, name, code string, price decimal.Decimal, id uint, eventID string) (*models.Product, error) {
	supplier, err := models.FindSupplier(ownerID)
	if err != nil || supplier == nil || !supplier.Enabled {
		return nil, errors.New("供应商不存在或已禁用")
	}
	item := models.NewProduct()
	item.SetID(id)
	item.SupplierID, item.Name, item.Code, item.Price = ownerID, name, code, price
	payload := models.ProductChangedPayload(eventID, ownerID, id, "created")
	outbox, err := models.NewProductOutbox(eventID, contract.EventProductChanged, contract.SubjectProductChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		// 商品与 Outbox 必须在同一 IDataAction 事务内成功。
		if err := item.InsertWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return item, err
}

func ProductSnapshot(id uint) (*supplierdto.ProductSnapshot, error) {
	product, err := models.FindProduct(id)
	if err != nil || product == nil || !product.Enabled {
		return nil, errors.New("商品不存在或未上架")
	}
	supplier, err := models.FindSupplier(product.SupplierID)
	if err != nil || supplier == nil || !supplier.Enabled {
		return nil, errors.New("供应商不存在或已禁用")
	}
	return &supplierdto.ProductSnapshot{ProductID: product.ID, SupplierID: product.SupplierID, SupplierName: supplier.Name,
		ProductCode: product.Code, ProductName: product.Name, UnitPrice: product.Price}, nil
}

func OwnedProducts(ownerID string) ([]*supplierdto.Product, error) {
	items, err := models.ListProducts()
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Product, 0)
	for _, item := range items {
		if item != nil && item.SupplierID == ownerID {
			result = append(result, ProductResponse(item))
		}
	}
	return result, nil
}

func UpdateOwnedProduct(ownerID string, id uint, price *decimal.Decimal, enabled *bool, eventID string) (*supplierdto.Product, error) {
	item, err := models.FindProduct(id)
	if err != nil || item == nil || item.SupplierID != ownerID {
		return nil, errors.New("商品不存在或无权操作")
	}
	if price != nil {
		if !price.GreaterThan(decimal.Zero) {
			return nil, errors.New("商品价格必须大于 0")
		}
		item.Price = *price
	}
	if enabled != nil {
		item.Enabled = *enabled
	}
	payload := models.ProductChangedPayload(eventID, ownerID, id, "updated")
	outbox, err := models.NewProductOutbox(eventID, contract.EventProductChanged, contract.SubjectProductChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return ProductResponse(item), err
}

func AvailableProducts(name, code, supplierID string) ([]*supplierdto.Product, error) {
	items, err := models.ListProducts()
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Product, 0, len(items))
	for _, item := range items {
		if item == nil || !item.Enabled || (supplierID != "" && item.SupplierID != supplierID) ||
			(code != "" && !strings.EqualFold(item.Code, code)) || (name != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(name))) {
			continue
		}
		supplier, findErr := models.FindSupplier(item.SupplierID)
		if findErr != nil || supplier == nil || !supplier.Enabled {
			continue
		}
		result = append(result, &supplierdto.Product{ID: item.ID, SupplierID: item.SupplierID, SupplierName: supplier.Name,
			Name: item.Name, Code: item.Code, Price: item.Price, Enabled: item.Enabled})
	}
	return result, nil
}
