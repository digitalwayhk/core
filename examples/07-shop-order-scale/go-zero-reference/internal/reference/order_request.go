// 本文件声明原生下单请求及业务校验，不嵌入 Core Router 或模型。
package reference

import (
	"errors"
	"github.com/shopspring/decimal"
	"strings"
)

// OrderRequest 保持 07 负载所需的外部输入及内部商品快照。
type OrderRequest struct {
	ProductID    uint            `json:"productID"`
	Quantity     int             `json:"quantity"`
	RequestID    string          `json:"requestID"`
	Address      Address         `json:"address"`
	OrderID      uint            `json:"orderID"`
	UserID       uint            `json:"userID"`
	SupplierID   uint            `json:"supplierID"`
	SupplierCode string          `json:"supplierCode"`
	SupplierName string          `json:"supplierName"`
	ProductCode  string          `json:"productCode"`
	ProductName  string          `json:"productName"`
	UnitPrice    decimal.Decimal `json:"unitPrice"`
}

func (r *OrderRequest) validate() error {
	r.RequestID = strings.TrimSpace(r.RequestID)
	if r.ProductID == 0 || r.Quantity <= 0 || r.RequestID == "" {
		return errors.New("下单参数不完整")
	}
	return nil
}
