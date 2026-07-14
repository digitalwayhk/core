package public

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// ProductItem 是公开商品列表允许返回的字段。
type ProductItem struct {
	ID    uint   `json:"id,string"`
	Name  string `json:"name"`
	Price string `json:"price"`
}

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

// Do 使用同一 SearchItem 组合筛选条件，并转换为最小公开结构。
func (own *GetProducts) Do(servertypes.IRequest) (interface{}, error) {
	list := entity.NewModelList[models.Product](nil)
	items, _, err := list.SearchAll(1, 500, func(search *persistencetypes.SearchItem) {
		if own.ID > 0 {
			search.AddWhereN("ID", own.ID)
		}
		if own.Name != "" {
			search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+own.Name+"%")
		}
	})
	if err != nil {
		return nil, err
	}
	result := make([]ProductItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, ProductItem{ID: item.ID, Name: item.Name, Price: item.Price.String()})
	}
	return result, nil
}

// RouterInfo 将商品查询注册为公开 GET 路由。
func (own *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = "GET"
	return info
}
