package test

import (
	"fmt"
	"github.com/digitalwayhk/core/models"
	msgModel "github.com/digitalwayhk/core/pkg/message/models"
	pkg "github.com/digitalwayhk/core/pkg/persistence/models"
	"testing"
)

func init() {

	pkg.GetRemoteDBHandler = func(dbconfig *pkg.RemoteDbConfig) {
		if dbconfig.Host == "" {
			dbconfig.Host = "127.0.0.1"
			dbconfig.Port = 27017
			dbconfig.User = "root"
			dbconfig.Pass = "123456"
			dbconfig.DataBaseType = "mongo"
		}
	}
}

func Test_Read(t *testing.T) {
	list := models.NewMongoModelList[msgModel.MsgRecord]()
	filter := list.GetSearchItem()
	filter.AddWhereN("userId", uint(0))

	if err := list.LoadList(filter); err != nil {
		fmt.Println("Load err:", err)
	}
	fmt.Println("List count: ", list.Count())
	if list.Count() > 0 {
		fmt.Printf("SearchResult:  %#v\n", list.ToArray()[0])
	}
}
