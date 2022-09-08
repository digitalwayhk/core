package analysis

import (
	"errors"
	"fmt"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type IAnalysisModel interface {
	SearchSQL() string
}

//TODO:处理行情时再补充
type AnalysisModel struct {
	IAnalysisModel `json:"-" gorm:"-"`
	dateLatitude   *DateLatitude
	timeLatitude   *TimeLatitude
	model          interface{}
	latitudes      []*LatitudeColumn
	indexs         []*IndexColumn
	MinID          uint
	MaxID          uint
	RowCount       uint
	DateLID        uint
	TimeLID        uint
	interval       int //统计运行间隔，单位分钟
}

func (own *AnalysisModel) Validation() error {
	if own.model == nil {
		return errors.New("model is nil")
	}
	if len(own.latitudes) == 0 {
		return errors.New("latitudes is empty")
	}
	if len(own.indexs) == 0 {
		return errors.New("indexs is empty")
	}
	return nil
}
func (own *AnalysisModel) Do() error {

	return nil
}
func (own *AnalysisModel) SearchSQL() string {
	tablename := utils.GetTypeName(own.model)
	if stn, ok := own.model.(types.IScopesTableName); ok {
		tablename = stn.TableName()
	}
	sql := "SELECT %s FROM %s WHERE CreatedAt>'%s' AND CreatedAt<='%s' GROUP BY %s"
	columns := ""
	groups := ""
	for _, latitude := range own.latitudes {
		columns += latitude.ColumnName + ","
		groups += latitude.ColumnName + ","
	}
	for _, index := range own.indexs {
		columns += index.ColumnName + ","
	}
	columns = columns[:len(columns)-1]
	groups = groups[:len(groups)-1]
	return fmt.Sprintf(sql, columns, tablename, "", "", groups)
}
