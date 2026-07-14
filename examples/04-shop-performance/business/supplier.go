package business

import "github.com/digitalwayhk/core/examples/04-shop-performance/models"

// SupplierService 处理供应商查询、引用保护和启停。
type SupplierService struct{}

// NewSupplierService 创建无状态供应商业务服务。
func NewSupplierService() *SupplierService { return &SupplierService{} }

// ListEnabled 查询公开可见的启用供应商。
func (own *SupplierService) ListEnabled(id uint, code, name string) ([]*models.Supplier, error) {
	return models.NewSupplier().QueryEnabled(id, code, name)
}

// ValidateCreate 校验新增供应商。
func (own *SupplierService) ValidateCreate(item *models.Supplier) error { return item.AddValid() }

// ValidateUpdate 校验修改供应商。
func (own *SupplierService) ValidateUpdate(item *models.Supplier, old interface{}) error {
	return item.UpdateValid(old)
}

// EnsureRemovable 阻止删除仍有商品的供应商。
func (own *SupplierService) EnsureRemovable(id uint) error {
	used, err := models.NewSupplier().HasProducts(id)
	if err != nil {
		return err
	}
	if used {
		return models.NewBusinessError("供应商已有商品，只能禁用")
	}
	return nil
}

// SetEnabled 启用或禁用供应商，不级联修改商品状态。
func (own *SupplierService) SetEnabled(id uint, enabled bool) (*models.Supplier, error) {
	item, err := models.NewSupplier().FindByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, models.NewBusinessError("供应商不存在")
	}
	if item.Enabled == enabled {
		return item, nil
	}
	item.Enabled = enabled
	if err := item.Update(); err != nil {
		return nil, err
	}
	return item, nil
}
