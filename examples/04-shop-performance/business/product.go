package business

import "github.com/digitalwayhk/core/examples/04-shop-performance/models"

// ProductService 处理商品校验和引用保护。
type ProductService struct{}

// NewProductService 创建无状态商品业务服务。
func NewProductService() *ProductService { return &ProductService{} }

// ListAvailable 查询商品和供应商均启用的公开商品。
func (own *ProductService) ListAvailable(id uint, code, name string, supplierID uint, supplierCode string) ([]*models.Product, error) {
	return models.NewProduct().QueryAvailable(id, code, name, supplierID, supplierCode)
}

// ValidateCreate 校验新增商品。
func (own *ProductService) ValidateCreate(product *models.Product) error {
	if err := product.AddValid(); err != nil {
		return err
	}
	return own.ensureSupplierEnabled(product.SupplierID)
}

// ValidateUpdate 校验修改商品。
func (own *ProductService) ValidateUpdate(product *models.Product, old interface{}) error {
	if err := product.UpdateValid(old); err != nil {
		return err
	}
	return own.ensureSupplierEnabled(product.SupplierID)
}

// EnsureRemovable 阻止删除已被历史订单引用的商品。
func (own *ProductService) EnsureRemovable(productID uint) error {
	if err := models.FlushOrders(); err != nil {
		return err
	}
	used, err := models.NewOrder().ExistsByProductID(productID)
	if err != nil {
		return err
	}
	if used {
		return models.NewBusinessError("商品已被订单使用，请保留历史数据")
	}
	return nil
}

// SetEnabled 启用或禁用商品；启用时供应商必须仍然有效。
func (own *ProductService) SetEnabled(id uint, enabled bool) (*models.Product, error) {
	product, err := models.NewProduct().FindByID(id)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, models.NewBusinessError("商品不存在")
	}
	if enabled {
		if err := own.ensureSupplierEnabled(product.SupplierID); err != nil {
			return nil, err
		}
	}
	if product.Enabled == enabled {
		return product, nil
	}
	product.Enabled = enabled
	if err := product.Update(); err != nil {
		return nil, err
	}
	return product, nil
}

func (own *ProductService) ensureSupplierEnabled(id uint) error {
	supplier, err := models.NewSupplier().FindByID(id)
	if err != nil {
		return err
	}
	if supplier == nil {
		return models.NewBusinessError("供应商不存在")
	}
	if !supplier.Enabled {
		return models.NewBusinessError("供应商已禁用")
	}
	return nil
}
