package models

import (
	"strings"

	"strconv"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type RemoteDbConfig struct {
	*entity.Model
	Name         string              `json:"name"`
	Host         string              `json:"host"`
	Port         uint                `json:"port"`
	ConMax       uint                `json:"conmax"`  //最大连接数
	ConPool      uint                `json:"conpool"` //连接池大小
	User         string              `json:"user"`
	Pass         string              `json:"pass"`
	TimeOut      uint                `json:"timeout"`
	ReadTimeOut  uint                `json:"readtimeout"`
	WriteTimeOut uint                `json:"writetimeout"`
	ConnectType  types.DBConnectType `json:"connecttype,int"` //连接类型
}

func (own *RemoteDbConfig) GetHash() string {
	return utils.HashCodes(own.Name, strconv.Itoa(int(own.ConnectType)))
}
func (own *RemoteDbConfig) GetLocalDBName() string {
	return strings.ToLower(utils.GetTypeName(own))
}
func NewRemoteDbConfig() *RemoteDbConfig {
	return &RemoteDbConfig{
		Model: entity.NewModel(),
	}
}

func (own *RemoteDbConfig) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// func (own *RemoteDbConfig) MarshalJSON() ([]byte, error) {
// 	type Alias RemoteDbConfig
// 	own.Pass = "******"
// 	return json.Marshal(&struct {
// 		*Alias
// 	}{
// 		Alias: (*Alias)(own),
// 	})
// }
// func (own *RemoteDbConfig) UnmarshalJSON(b []byte) error {
// 	type Alias RemoteDbConfig
// 	aux := &struct {
// 		*Alias
// 	}{
// 		Alias: (*Alias)(own),
// 	}
// 	if err := json.Unmarshal(b, &aux); err != nil {
// 		return err
// 	}
// 	return nil
// }
