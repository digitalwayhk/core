package manage

import (
	"github.com/digitalwayhk/core/internal/manage/view"
	"github.com/digitalwayhk/core/internal/models"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	st "github.com/digitalwayhk/core/pkg/server/types"
)

type IManageService interface {
	Routers() []st.IRouter
	ParseBefore(sender interface{}, req st.IRequest) error
	ParseAfter(sender interface{}, req st.IRequest) error
	ValidationBefore(sender interface{}, req st.IRequest) (error, bool)
	ValidationAfter(sender interface{}, req st.IRequest) error
	//add\edit\remove\submit\release 标准do操作的前置操作
	DoBefore(sender interface{}, req st.IRequest) (interface{}, error, bool)
	//add\edit\remove\submit\release 标准do操作的后置操作
	DoAfter(sender interface{}, req st.IRequest) (interface{}, error)
}
type IManageView interface {
	GetView() *view.ViewModel
	ViewModel(model *view.ViewModel)
	ViewFieldModel(model interface{}, field *view.FieldModel)
	ViewCommandModel(cmd *view.CommandModel)
	ViewChildModel(child *view.ViewChildModel)
}
type IManageSearch interface {
	SearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool)
	SearchAfter(sender, result interface{}, req st.IRequest) (interface{}, error)
	ForeignSearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool)
	ForeignSearchAfter(sender, result interface{}, req st.IRequest) (interface{}, error)
	ChildSearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool)
	ChildSearchAfter(sender, result interface{}, req st.IRequest) (interface{}, error)
	OnSearchData(list interface{}, total int64) *view.TableData
}
type IGetModelList[T pt.IModel] interface {
	GetList() *models.ModelList[T]
}

type ManageService[T pt.IModel] struct {
	View    *View[T]
	Search  *Search[T]
	Add     *Add[T]
	Edit    *Edit[T]
	Remove  *Remove[T]
	Submit  *Submit[T]
	Release *Release[T]
	Req     st.IRequest
}

func NewManageService[T pt.IModel](instance interface{}) *ManageService[T] {
	own := &ManageService[T]{}
	own.View = NewView[T](instance)
	own.Search = NewSearch[T](instance)
	own.Add = NewAdd[T](instance)
	own.Edit = NewEdit[T](instance)
	own.Remove = NewRemove[T](instance)
	own.Submit = NewSubmit[T](instance)
	own.Release = NewRelease[T](instance)
	return own
}
func (own *ManageService[T]) Routers() []st.IRouter {
	return []st.IRouter{
		own.View,
		own.Search,
		own.Add,
		own.Edit,
		own.Remove,
		own.Submit,
		own.Release,
	}
}
func (own *ManageService[T]) GetView() *view.ViewModel {
	return own.View.Model
}
func (own *ManageService[T]) GetList() *models.ModelList[T] {
	return models.NewManageModelList[T]()
}
func (own *ManageService[T]) ParseBefore(sender interface{}, req st.IRequest) error {
	own.Req = req
	return nil
}
func (own *ManageService[T]) ParseAfter(sender interface{}, req st.IRequest) error {
	own.Req = req
	return nil
}
func (own *ManageService[T]) ValidationBefore(sender interface{}, req st.IRequest) (error, bool) {
	own.Req = req
	return nil, false
}
func (own *ManageService[T]) ValidationAfter(sender interface{}, req st.IRequest) error {
	own.Req = req
	return nil
}

func (own *ManageService[T]) DoBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	own.Req = req
	return nil, nil, false
}
func (own *ManageService[T]) DoAfter(sender interface{}, req st.IRequest) (interface{}, error) {
	own.Req = req
	return nil, nil
}

func (own *ManageService[T]) ViewModel(model *view.ViewModel) {}
func (own *ManageService[T]) ViewFieldModel(model interface{}, field *view.FieldModel) {

}
func (own *ManageService[T]) ViewCommandModel(cmd *view.CommandModel)   {}
func (own *ManageService[T]) ViewChildModel(child *view.ViewChildModel) {}

func (own *ManageService[T]) SearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	own.Req = req
	return nil, nil, false
}
func (own *ManageService[T]) SearchAfter(sender, result interface{}, req st.IRequest) (interface{}, error) {
	own.Req = req
	return result, nil
}
func (own *ManageService[T]) ForeignSearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	own.Req = req
	return nil, nil, false
}
func (own *ManageService[T]) ForeignSearchAfter(sender, result interface{}, req st.IRequest) (interface{}, error) {
	own.Req = req
	return result, nil
}
func (own *ManageService[T]) ChildSearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	own.Req = req
	return nil, nil, false
}
func (own *ManageService[T]) ChildSearchAfter(sender, result interface{}, req st.IRequest) (interface{}, error) {
	own.Req = req
	return result, nil
}
func (own *ManageService[T]) OnSearchData(list interface{}, total int64) *view.TableData {
	data := &view.TableData{
		Rows:  list,
		Total: total,
	}
	return data
}
