package business

import "github.com/digitalwayhk/core/examples/02-shop-payment/models"

// ProductService 处理商品校验和引用保护。
type ProductService struct{}

// NewProductService 创建无状态商品业务服务。
func NewProductService() *ProductService { return &ProductService{} }

// Query 按可选 ID 和名称查询商品。
func (own *ProductService) Query(id uint, name string) ([]*models.Product, error) {
	return models.NewProduct().Query(id, name)
}

// ValidateCreate 校验新增商品。
func (own *ProductService) ValidateCreate(product *models.Product) error { return product.AddValid() }

// ValidateUpdate 校验修改商品。
func (own *ProductService) ValidateUpdate(product *models.Product, old interface{}) error {
	return product.UpdateValid(old)
}

// EnsureRemovable 阻止删除已被历史订单引用的商品。
func (own *ProductService) EnsureRemovable(productID uint) error {
	used, err := models.NewOrder().ExistsByProductID(productID)
	if err != nil {
		return err
	}
	if used {
		return models.NewBusinessError("商品已被订单使用，请保留历史数据")
	}
	return nil
}
