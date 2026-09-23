package public

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/api/dto"
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetCategories 查询全部分类，或按可选 ID 与名称组合筛选。
type GetCategories struct {
	ID   uint
	Name string
}

// Parse 读取可选的 id 精确条件和 name 模糊条件。
func (own *GetCategories) Parse(req servertypes.IRequest) error {
	own.Name = strings.TrimSpace(req.GetValue("name"))
	id := strings.TrimSpace(req.GetValue("id"))
	if id == "" {
		return nil
	}
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return models.NewBusinessError("分类 ID 格式错误")
	}
	own.ID = uint(value)
	return nil
}

// Validation 接受空筛选条件，此时返回全部分类。
func (*GetCategories) Validation(servertypes.IRequest) error { return nil }

// Do 通过 Category 模型的直接查询方法组合筛选，并转换为最小公开结构。
func (own *GetCategories) Do(servertypes.IRequest) (interface{}, error) {
	items, err := models.NewCategory().Query(own.ID, own.Name)
	if err != nil {
		return nil, err
	}
	return dto.CategoryResponses(items), nil
}

// GetResponse 返回 OpenAPI 用的分类列表成功响应结构。
func (*GetCategories) GetResponse() interface{} {
	return []*dto.CategoryResponse{}
}

// RouterInfo 将分类查询注册为公开 GET 路由。
func (own *GetCategories) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
}
