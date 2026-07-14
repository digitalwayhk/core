package public

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/dto"
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetProducts 查询全部商品，或按可选 ID 与名称组合筛选。
type GetProducts struct {
	ID   uint
	Name string
}

// Parse 读取可选的 id 精确条件和 name 模糊条件。
func (own *GetProducts) Parse(req servertypes.IRequest) error {
	own.Name = strings.TrimSpace(req.GetValue("name"))
	id := strings.TrimSpace(req.GetValue("id"))
	if id == "" {
		return nil
	}
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return models.NewBusinessError("商品 ID 格式错误")
	}
	own.ID = uint(value)
	return nil
}

// Validation 接受空筛选条件，此时返回全部商品。
func (own *GetProducts) Validation(servertypes.IRequest) error {
	return nil
}

// Do 通过 Product 模型的直接查询方法组合筛选，并转换为最小公开结构。
func (own *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	items, err := models.NewProduct().Query(own.ID, own.Name)
	if err != nil {
		return nil, err
	}
	return dto.ProductResponses(items), nil
}

// GetResponse 返回 OpenAPI 用的商品列表成功响应结构。
func (own *GetProducts) GetResponse() interface{} {
	return []*dto.ProductResponse{}
}

// RouterInfo 将商品查询注册为公开 GET 路由。
func (own *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = "GET"
	return info
}
