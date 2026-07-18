// 本文件提供当前服务供其他服务调用的 Public API 或入口 facade 能力。
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

// GetProducts 定义本文件能力使用的核心结构。
type GetProducts struct {
	ID         uint
	Name, Code string
	SupplierID uint
}

// Parse 实现本类型在当前服务边界中的行为。
func (own *GetProducts) Parse(req servertypes.IRequest) error {
	own.Name, own.Code = strings.TrimSpace(req.GetValue("name")), strings.TrimSpace(req.GetValue("code"))
	for key, target := range map[string]*uint{"id": &own.ID, "supplierID": &own.SupplierID} {
		if value := strings.TrimSpace(req.GetValue(key)); value != "" {
			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}
			*target = uint(parsed)
		}
	}
	return nil
}

// Validation 实现本类型在当前服务边界中的行为。
func (*GetProducts) Validation(servertypes.IRequest) error { return nil }

// Do 实现本类型在当前服务边界中的行为。
func (own *GetProducts) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&supplierapi.GetProducts{ID: own.ID, Name: own.Name, Code: own.Code, SupplierID: own.SupplierID})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	items := []*supplierdto.Product{}
	response.GetData(&items)
	return items, nil
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetProducts) GetResponse() interface{} { return []*supplierdto.Product{} }

// GetCacheKey 实现本类型在当前服务边界中的行为。
func (own *GetProducts) GetCacheKey() string {
	return utils.HashCodes(strconv.FormatUint(uint64(own.ID), 10), strings.ToLower(own.Name), strings.ToLower(own.Code), strconv.FormatUint(uint64(own.SupplierID), 10))
}

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
	info.UseCache(30 * time.Second)
	return info
}
