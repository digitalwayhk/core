package test

import (
	"testing"

	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

func Test_Utils(t *testing.T) {
	items := make([]*models.OrderModel, 0)
	obj := utils.NewArrayItem(&items)

	if b, ok := obj.(types.IDBName); ok {
		t.Log(b)
	} else {
		t.Log("not IDBName", obj)
	}
}
