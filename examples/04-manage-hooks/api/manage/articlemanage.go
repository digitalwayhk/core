package manage

import (
	"errors"

	"github.com/digitalwayhk/core/examples/04-manage-hooks/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ArticleManage 通过重写 hook 扩展标准 CRUD。
type ArticleManage struct {
	*managepkg.ManageService[models.Article]
}

func NewArticleManage() *ArticleManage {
	own := &ArticleManage{}
	own.ManageService = managepkg.NewManageService[models.Article](own)
	return own
}

// DoBefore 在标准操作执行前统一校验。
func (own *ArticleManage) DoBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
	// sender 是 Add/Edit/Remove/Submit/Release 等操作对象。
	// 示例只演示统一入口，实际项目可按 sender 类型分支处理。
	if add, ok := sender.(*managepkg.Add[models.Article]); ok {
		if add.Model != nil && (*add.Model).Title == "" {
			return nil, errors.New("标题不能为空"), false
		}
	}
	return nil, nil, false
}

// SearchBefore 可在查询前补充默认条件。
func (own *ArticleManage) SearchBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
	// 返回 stop=true 可完全接管查询；本示例继续走默认查询流程。
	return nil, nil, false
}

// SearchAfter 可整理返回给前端的数据。
func (own *ArticleManage) SearchAfter(sender interface{}, result *view.TableData, req types.IRequest) (interface{}, error) {
	return result, nil
}

func (own *ArticleManage) ViewModel(model *view.ViewModel) {
	model.Title = "文章管理"
	model.AutoLoad = true
}
