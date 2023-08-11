package test

import (
	"testing"

	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
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

func Test_AddOrders(t *testing.T) {
	list := entity.NewModelList[models.OrderModel](nil)
	for i := 0; i <= 100; i++ {
		row := list.NewItem()
		row.ID = uint(i)
		err := list.Add(row)
		if err != nil {
			t.Error(err)
		}
		err = list.Save()
		if err != nil {
			t.Error(err)
		}
	}
	_, total, _ := list.SearchAll(1, 1)
	assert.Equal(t, total, 100)
}

func Benchmark_AddOrders(b *testing.B) {
	list := entity.NewModelList[models.OrderModel](nil)
	for i := 0; i < b.N; i++ {

		row := list.NewItem()
		row.ID = uint(b.N + 1*i)
		err := list.Add(row)
		if err != nil {
			b.Error(err)
		}
		err = list.Save()
		if err != nil {
			b.Error(err)
		}
	}
}
