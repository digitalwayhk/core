package openapi

import (
	"github.com/digitalwayhk/core/examples/05-openapi-response/api/public"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// InvoiceService 注册发票相关 API。
type InvoiceService struct{}

func (own *InvoiceService) ServiceName() string { return "invoice" }

func (own *InvoiceService) Routers() []types.IRouter {
	return []types.IRouter{&public.CreateInvoice{}}
}

func (own *InvoiceService) SubscribeRouters() []*types.ObserveArgs { return nil }
