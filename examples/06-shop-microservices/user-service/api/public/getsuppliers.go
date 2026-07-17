package public

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	supplierapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type GetSuppliers struct {
	ID         uint
	Code, Name string
}

func (own *GetSuppliers) Parse(req servertypes.IRequest) error {
	own.Code, own.Name = strings.TrimSpace(req.GetValue("code")), strings.TrimSpace(req.GetValue("name"))
	if value := strings.TrimSpace(req.GetValue("id")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		own.ID = uint(parsed)
	}
	return nil
}
func (*GetSuppliers) Validation(servertypes.IRequest) error { return nil }
func (own *GetSuppliers) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&supplierapi.GetSuppliers{ID: own.ID, Code: own.Code, Name: own.Name})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	items := []*supplierdto.Supplier{}
	response.GetData(&items)
	return items, nil
}
func (*GetSuppliers) GetResponse() interface{} { return []*supplierdto.Supplier{} }
func (own *GetSuppliers) GetCacheKey() string {
	return utils.HashCodes(strconv.FormatUint(uint64(own.ID), 10), strings.ToLower(own.Code), strings.ToLower(own.Name))
}
func (own *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
	info.UseCache(30 * time.Second)
	return info
}
