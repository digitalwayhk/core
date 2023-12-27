package test

import (
	"log"
	"testing"

	"github.com/digitalwayhk/core/examples/demo/api/private"
	"github.com/digitalwayhk/core/pkg/utils"
)

func Test_RouterInfo_New(t *testing.T) {
	info := &private.AddOrder{}
	api := info.RouterInfo().New()
	if order, ok := api.(*private.AddOrder); ok {
		order.Price = "123"
	}
	as := utils.PrintObj(api)
	log.Println(as)
	log.Println(&as)
	api1 := info.RouterInfo().New()
	as1 := utils.PrintObj(api1)
	log.Println(as1)
	log.Println(&as1)
}
