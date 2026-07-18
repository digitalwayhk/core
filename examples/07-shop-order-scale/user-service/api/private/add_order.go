// Package private 提供 07 用户入口服务买家下单 API。
package private

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	orderapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	supplierapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

var userOrderIDByRequest sync.Map

// AddOrder 是买家下单入口。
type AddOrder struct {
	ProductID uint                    `json:"productID"`
	Quantity  int                     `json:"quantity"`
	RequestID string                  `json:"requestID"`
	Address   userdto.AddressSnapshot `json:"address"`
}

// Parse 绑定下单请求。
func (own *AddOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验下单请求。
func (own *AddOrder) Validation(servertypes.IRequest) error {
	if own.ProductID == 0 || own.Quantity <= 0 || strings.TrimSpace(own.RequestID) == "" {
		return errors.New("下单参数不完整")
	}
	return nil
}

// Do 查询商品快照后调用订单服务内部下单 API。
func (own *AddOrder) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	userID64, err := strconv.ParseUint(uid, 10, 64)
	if err != nil || userID64 == 0 {
		return nil, errors.New("用户身份无效")
	}
	productRes, err := req.CallService(&supplierapi.GetProducts{ID: own.ProductID})
	if err != nil || !productRes.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, productRes.GetError()
	}
	var products []*supplierdto.Product
	productRes.GetData(&products)
	if len(products) == 0 || products[0] == nil {
		return nil, errors.New("商品不存在")
	}
	orderID := orderIDForRequest(uint(userID64), strings.TrimSpace(own.RequestID), req)
	orderRes, err := req.CallService(&orderapi.CreateOrder{
		OrderID:      orderID,
		UserID:       uint(userID64),
		SupplierID:   products[0].SupplierID,
		ProductID:    products[0].ID,
		Quantity:     own.Quantity,
		RequestID:    own.RequestID,
		SupplierCode: products[0].SupplierCode,
		SupplierName: products[0].SupplierName,
		ProductCode:  products[0].Code,
		ProductName:  products[0].Name,
		UnitPrice:    products[0].Price,
		Address:      own.Address,
	})
	if err != nil || !orderRes.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, orderRes.GetError()
	}
	var order orderdto.Order
	orderRes.GetData(&order)
	return &order, nil
}

func orderIDForRequest(userID uint, requestID string, req interface{ NewID() uint }) uint {
	key := strconv.FormatUint(uint64(userID), 10) + ":" + strings.TrimSpace(requestID)
	if value, ok := userOrderIDByRequest.Load(key); ok {
		if id, ok := value.(uint); ok && id > 0 {
			return id
		}
	}
	id := req.NewID()
	actual, _ := userOrderIDByRequest.LoadOrStore(key, id)
	if stored, ok := actual.(uint); ok && stored > 0 {
		return stored
	}
	return id
}

// GetResponse 返回订单响应 DTO 类型。
func (*AddOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回买家下单 Private 路由信息。
func (own *AddOrder) RouterInfo() *servertypes.RouterInfo {
	return userPrivateRoute(own, "addorder", http.MethodPost)
}
