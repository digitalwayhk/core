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

// ProductListResponse 执行本文件能力对应的业务操作。
func ProductListResponse() interface{} { return []*supplierdto.Product{} }

// SupplierListResponse 执行本文件能力对应的业务操作。
func SupplierListResponse() interface{} { return []*supplierdto.Supplier{} }

// AvailableSuppliers 执行本文件能力对应的业务操作。
func AvailableSuppliers(id uint, code, name string) ([]*supplierdto.Supplier, error) {
	items, err := models.ListSuppliers()
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Supplier, 0, len(items))
	for _, item := range items {
		if item == nil || !item.Enabled || (id != 0 && item.ID != id) ||
			(code != "" && !strings.EqualFold(item.Code, code)) ||
			(name != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(name))) {
			continue
		}
		result = append(result, &supplierdto.Supplier{ID: item.ID, Code: item.Code, Name: item.Name, Description: item.Description, Enabled: true})
	}
	return result, nil
}

// ProductResponse 执行本文件能力对应的业务操作。
func ProductResponse(item *models.Product) *supplierdto.Product {
	if item == nil {
		return nil
	}
	supplier, _ := models.FindSupplierByID(item.SupplierID)
	name, code := "", ""
	if supplier != nil {
		name = supplier.Name
		code = supplier.Code
	}
	return &supplierdto.Product{ID: item.ID, SupplierID: item.SupplierID, SupplierCode: code, SupplierName: name, Name: item.Name, Code: item.Code, Price: item.Price, Enabled: item.Enabled}
}

// EnsureSupplier 执行本文件能力对应的业务操作。
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
	item.AuthUserID, item.Name, item.Code, item.Enabled = userID, strings.TrimSpace(name), "supplier-"+strings.ToLower(userID), true
	if item.Name == "" {
		item.Name = userID
	}
	return item, item.Save()
}

// UpdateSupplier 执行本文件能力对应的业务操作。
func UpdateSupplier(id uint, name string, enabled bool, traceID, eventID string) (*models.Supplier, error) {
	traceID = strings.TrimSpace(traceID)
	item, err := models.FindSupplierByID(id)
	if err != nil || item == nil {
		return nil, errors.New("供应商不存在")
	}
	item.Name = strings.TrimSpace(name)
	item.Enabled = enabled
	item.TraceID = traceID
	payload := models.SupplierChangedPayload(traceID, eventID, item.ID, "updated")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventSupplierChanged, contract.SubjectSupplierChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return item, err
}

// UpdateSupplierDetails 只修改供应商可维护的公开资料，身份与启用状态保持不变。
func UpdateSupplierDetails(id uint, name, code, description, traceID, eventID string) (*models.Supplier, error) {
	traceID = strings.TrimSpace(traceID)
	item, err := models.FindSupplierByID(id)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound
	}
	item.Name = strings.TrimSpace(name)
	item.Code = strings.ToLower(strings.TrimSpace(code))
	item.Description = strings.TrimSpace(description)
	item.TraceID = traceID
	payload := models.SupplierChangedPayload(traceID, eventID, item.ID, "updated")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventSupplierChanged, contract.SubjectSupplierChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return item, err
}

// SetSupplierEnabled 由管理员受控命令调用，不与公开资料编辑混用。
func SetSupplierEnabled(id uint, enabled bool, traceID, eventID string) (*models.Supplier, error) {
	traceID = strings.TrimSpace(traceID)
	item, err := models.FindSupplierByID(id)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound
	}
	item.Enabled = enabled
	item.TraceID = traceID
	payload := models.SupplierChangedPayload(traceID, eventID, item.ID, "enabled_changed")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventSupplierChanged, contract.SubjectSupplierChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return item, err
}

// CreateProduct 执行本文件能力对应的业务操作。
func CreateProduct(ownerID uint, name, code string, price decimal.Decimal, id uint, traceID, eventID string) (*models.Product, error) {
	traceID = strings.TrimSpace(traceID)
	supplier, err := models.FindSupplierByID(ownerID)
	if err != nil || supplier == nil || !supplier.Enabled {
		return nil, errors.New("供应商不存在或已禁用")
	}
	item := models.NewProduct()
	item.SetID(id)
	item.TraceID = traceID
	item.SupplierID, item.Name, item.Code, item.Price = ownerID, name, code, price
	payload := models.ProductChangedPayload(traceID, eventID, ownerID, id, "created")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventProductChanged, contract.SubjectProductChanged, payload)
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

// ProductSnapshot 执行本文件能力对应的业务操作。
func ProductSnapshot(id uint) (*supplierdto.ProductSnapshot, error) {
	product, err := models.FindProduct(id)
	if err != nil || product == nil || !product.Enabled {
		return nil, errors.New("商品不存在或未上架")
	}
	supplier, err := models.FindSupplierByID(product.SupplierID)
	if err != nil || supplier == nil || !supplier.Enabled {
		return nil, errors.New("供应商不存在或已禁用")
	}
	return &supplierdto.ProductSnapshot{ProductID: product.ID, SupplierID: product.SupplierID, SupplierCode: supplier.Code, SupplierName: supplier.Name,
		ProductCode: product.Code, ProductName: product.Name, UnitPrice: product.Price}, nil
}

// OwnedProducts 执行本文件能力对应的业务操作。
func OwnedProducts(ownerID uint) ([]*supplierdto.Product, error) {
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

// UpdateOwnedProduct 执行本文件能力对应的业务操作。
func UpdateOwnedProduct(ownerID uint, id uint, price *decimal.Decimal, enabled *bool, traceID, eventID string) (*supplierdto.Product, error) {
	traceID = strings.TrimSpace(traceID)
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
	item.TraceID = traceID
	payload := models.ProductChangedPayload(traceID, eventID, ownerID, id, "updated")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventProductChanged, contract.SubjectProductChanged, payload)
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

// UpdateProduct 只更新通用编辑允许的商品资料，归属和上下架状态保持不变。
func UpdateProduct(id uint, name, code string, price decimal.Decimal, traceID, eventID string) (*models.Product, error) {
	traceID = strings.TrimSpace(traceID)
	item, err := models.FindProduct(id)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound
	}
	item.Name = strings.TrimSpace(name)
	item.Code = strings.ToLower(strings.TrimSpace(code))
	item.Price = price
	item.TraceID = traceID
	payload := models.ProductChangedPayload(traceID, eventID, item.SupplierID, item.ID, "updated")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventProductChanged, contract.SubjectProductChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return item, err
}

// SetProductEnabled 由供应商或管理员受控命令调用；上架时供应商必须有效。
func SetProductEnabled(id uint, enabled bool, traceID, eventID string) (*models.Product, error) {
	traceID = strings.TrimSpace(traceID)
	item, err := models.FindProduct(id)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound
	}
	supplier, err := models.FindSupplierByID(item.SupplierID)
	if err != nil || supplier == nil {
		return nil, contract.ErrResourceNotFound
	}
	if enabled && !supplier.Enabled {
		return nil, contract.ErrSubjectDisabled
	}
	item.Enabled = enabled
	item.TraceID = traceID
	payload := models.ProductChangedPayload(traceID, eventID, item.SupplierID, item.ID, "enabled_changed")
	outbox, err := models.NewProductOutbox(traceID, eventID, contract.EventProductChanged, contract.SubjectProductChanged, payload)
	if err != nil {
		return nil, err
	}
	err = models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := item.UpdateWith(action); err != nil {
			return err
		}
		return action.Insert(outbox)
	})
	return item, err
}

// AvailableProducts 执行本文件能力对应的业务操作。
func AvailableProducts(id uint, name, code string, supplierID uint) ([]*supplierdto.Product, error) {
	items, err := models.ListProducts()
	if err != nil {
		return nil, err
	}
	result := make([]*supplierdto.Product, 0, len(items))
	for _, item := range items {
		if item == nil || !item.Enabled || (id != 0 && item.ID != id) || (supplierID != 0 && item.SupplierID != supplierID) ||
			(code != "" && !strings.EqualFold(item.Code, code)) || (name != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(name))) {
			continue
		}
		supplier, findErr := models.FindSupplierByID(item.SupplierID)
		if findErr != nil || supplier == nil || !supplier.Enabled {
			continue
		}
		result = append(result, &supplierdto.Product{ID: item.ID, SupplierID: item.SupplierID, SupplierCode: supplier.Code, SupplierName: supplier.Name,
			Name: item.Name, Code: item.Code, Price: item.Price, Enabled: item.Enabled})
	}
	return result, nil
}
