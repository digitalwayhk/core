package public

import (
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// CreateInvoice 演示一个带明确响应结构的 public API。
type CreateInvoice struct {
	UserID string `json:"userid" desc:"用户 ID"`
	Amount int64  `json:"amount" desc:"金额，单位分"`
}

// CreateInvoiceResponse 是 OpenAPI 文档中展示的响应模型。
type CreateInvoiceResponse struct {
	InvoiceNo string `json:"invoiceNo" desc:"发票号"`
	State     string `json:"state" desc:"发票状态"`
	CreatedAt string `json:"createdAt" desc:"创建时间"`
}

func (own *CreateInvoice) Parse(req types.IRequest) error {
	return req.Bind(own)
}

func (own *CreateInvoice) Validation(req types.IRequest) error {
	if own.UserID == "" {
		return errors.New("userid 不能为空")
	}
	if own.Amount <= 0 {
		return errors.New("amount 必须大于 0")
	}
	return nil
}

func (own *CreateInvoice) Do(req types.IRequest) (interface{}, error) {
	return &CreateInvoiceResponse{
		InvoiceNo: "INV-" + time.Now().Format("20060102150405"),
		State:     "created",
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (own *CreateInvoice) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}

// GetResponse 告诉 OpenAPI 生成器该 API 的响应结构。
func (own *CreateInvoice) GetResponse() interface{} {
	return &CreateInvoiceResponse{}
}
