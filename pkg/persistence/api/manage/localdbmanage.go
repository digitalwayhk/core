package manage

import (
	"github.com/digitalwayhk/core/internal/manage"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
)

type DBData struct {
	*entity.Model
	Name       string
	Num        int     //库数量
	Size       float64 //库大小
	UpdateTime int32   //数据最后更新时间
	IsBreakup  bool    //是否拆分
	BreakupCol string  //拆分字段
}

func (own DBData) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

type LocalDBManage struct {
	*manage.ManageService[DBData]
}

func NewLocalDBManage() *LocalDBManage {
	own := &LocalDBManage{}
	own.ManageService = manage.NewManageService[DBData](own)
	return own
}
