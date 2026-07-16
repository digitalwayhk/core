package private

import (
	"errors"
	"net/http"
	"strings"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/shopspring/decimal"
)

func trustedSupplier(req servertypes.IRequest) (string, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return "", errors.New("供应商身份无效")
	}
	return uid, nil
}

// GetProductSnapshot 供 Order Service 在下单时获取可售商品快照。
type GetProductSnapshot struct {
	ProductID uint `json:"productID"`
}

func (g *GetProductSnapshot) Parse(req servertypes.IRequest) error { return req.Bind(g) }
func (g *GetProductSnapshot) Validation(servertypes.IRequest) error {
	if g.ProductID == 0 {
		return errors.New("商品 ID 不能为空")
	}
	return nil
}
func (g *GetProductSnapshot) Do(servertypes.IRequest) (interface{}, error) {
	return business.ProductSnapshot(g.ProductID)
}
func (*GetProductSnapshot) GetResponse() interface{}              { return &supplierdto.ProductSnapshot{} }
func (g *GetProductSnapshot) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(g) }

// AddProduct 为当前供应商新增默认下架的商品。
type AddProduct struct {
	Name  string          `json:"name"`
	Code  string          `json:"code"`
	Price decimal.Decimal `json:"price"`
}

func (a *AddProduct) Parse(req servertypes.IRequest) error { return req.Bind(a) }
func (a *AddProduct) Validation(req servertypes.IRequest) error {
	_, err := trustedSupplier(req)
	return err
}
func (a *AddProduct) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedSupplier(req)
	item, err := business.CreateProduct(uid, a.Name, a.Code, a.Price, req.NewID(), stringID(req.NewID()))
	if err != nil {
		return nil, err
	}
	return business.ProductResponse(item), nil
}
func (*AddProduct) GetResponse() interface{}              { return &supplierdto.Product{} }
func (a *AddProduct) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(a) }

// SetProduct 仅允许所有者修改价格或上下架。
type SetProduct struct {
	ProductID uint             `json:"productID"`
	Price     *decimal.Decimal `json:"price,omitempty"`
	Enabled   *bool            `json:"enabled,omitempty"`
}

func (s *SetProduct) Parse(req servertypes.IRequest) error { return req.Bind(s) }
func (s *SetProduct) Validation(req servertypes.IRequest) error {
	if s.ProductID == 0 {
		return errors.New("商品 ID 不能为空")
	}
	_, err := trustedSupplier(req)
	return err
}
func (s *SetProduct) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedSupplier(req)
	return business.UpdateOwnedProduct(uid, s.ProductID, s.Price, s.Enabled, stringID(req.NewID()))
}
func (*SetProduct) GetResponse() interface{}              { return &supplierdto.Product{} }
func (s *SetProduct) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(s) }

// GetMyProducts 返回当前供应商的全部商品，包含下架商品。
type GetMyProducts struct{}

func (*GetMyProducts) Parse(servertypes.IRequest) error { return nil }
func (*GetMyProducts) Validation(req servertypes.IRequest) error {
	_, err := trustedSupplier(req)
	return err
}
func (*GetMyProducts) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedSupplier(req)
	return business.OwnedProducts(uid)
}
func (*GetMyProducts) GetResponse() interface{} { return []*supplierdto.Product{} }
func (g *GetMyProducts) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}

func stringID(value uint) string { return strings.TrimSpace(models.EventID(value)) }
