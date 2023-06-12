package test

import (
	"testing"

	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

func init() {
	utils.TESTPATH = "/Users/vincent/Documents/Documents/MyCode/digitalway.hk/core/examples/demo/models/test"
}

func Test_Order(t *testing.T) {
	list := entity.NewModelList[models.TokenModel](nil)
	token := models.NewTokenModel()
	token.Name = "BTC"
	token.Orders = make([]*models.OrderModel, 0)
	order := models.NewOrderModel()
	order.Amount = decimal.NewFromFloat(1)
	order.Price = decimal.NewFromFloat(100)
	token.Orders = append(token.Orders, order)
	t.Log(utils.PrintObj(token))
	err := list.Add(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := list.Save(); err != nil {
		t.Fatal(err)
	}
	row, err := list.SearchId(token.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(utils.PrintObj(row))
}
