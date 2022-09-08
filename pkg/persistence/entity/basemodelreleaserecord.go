package entity

import "github.com/digitalwayhk/core/pkg/utils"

//基础数据发布记录
type BaseModelReleaseRecord struct {
	*Model
	SourceID        uint `json:"sourceid,string" gorm:"column:sourceid"`
	RealeaseAddress string
	RealeaseContent string
	RealeaseState   int
}

var snow = utils.NewAlgorithmSnowFlake(1000, 1000)

func (own *BaseModelReleaseRecord) NewModel() {
	if own.Model == nil {
		own.Model = NewModel()
		own.ID = uint(snow.NextId())
	}
}
func NewReleaseRecord() *BaseModelReleaseRecord {
	own := &BaseModelReleaseRecord{
		Model: NewModel(),
	}
	own.ID = uint(snow.NextId())
	return own
}
