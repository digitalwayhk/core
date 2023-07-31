package test

import (
	"testing"

	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

func init() {
	utils.TESTPATH = "/Users/vincent/Documents/Documents/MyCode/digitalway.hk/core/examples/demo/test"
}
func Test_Utils(t *testing.T) {
	items := make([]*models.OrderModel, 0)
	obj := utils.NewArrayItem(&items)

	if b, ok := obj.(types.IDBName); ok {
		t.Log(b)
	} else {
		t.Log("not IDBName", obj)
	}
}
func Test_searchOrders(t *testing.T) {
	list := entity.NewModelList[models.OrderModel](nil)
	rows, err := list.SearchWhere("ParnetID", uint(1))
	if err != nil {
		t.Error(err)
	}
	t.Log(rows)
}
