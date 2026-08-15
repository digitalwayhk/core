// 本文件验证商品目录只返回供应商有效的可下单商品，并补齐供应商快照。
package business

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "core-07-supplier-business-")
	if err != nil {
		panic(err)
	}
	utils.TESTPATH = root
	config.INITSERVER = false
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func TestListProductsFiltersMissingSupplierAndEnrichesSnapshot(t *testing.T) {
	unique := uint(time.Now().UnixNano() % 1_000_000_000)
	supplier := models.NewSupplier()
	supplier.ID = unique + 1
	supplier.UserID = unique + 2
	supplier.Code = fmt.Sprintf("supplier-%d", unique)
	supplier.Name = "有效供应商"
	supplier.Enabled = true

	valid := models.NewProduct()
	valid.ID = unique + 3
	valid.SupplierID = supplier.ID
	valid.Code = fmt.Sprintf("valid-product-%d", unique)
	valid.Name = "有效商品"
	valid.Price = decimal.NewFromInt(7)
	valid.Enabled = true

	legacyInvalid := models.NewProduct()
	legacyInvalid.ID = unique + 4
	legacyInvalid.SupplierID = 0
	legacyInvalid.Code = fmt.Sprintf("invalid-product-%d", unique)
	legacyInvalid.Name = "历史脏商品"
	legacyInvalid.Price = decimal.NewFromInt(7)
	legacyInvalid.Enabled = true

	require.NoError(t, models.RunTransaction(func(action persistencetypes.IDataAction) error {
		if err := supplier.InsertWith(action); err != nil {
			return err
		}
		if err := valid.InsertWith(action); err != nil {
			return err
		}
		return action.Insert(legacyInvalid)
	}))

	items, err := ListProducts(0, true)
	require.NoError(t, err)
	encoded, err := json.Marshal(items)
	require.NoError(t, err)
	var snapshots []map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &snapshots))
	require.Len(t, snapshots, 1)
	require.Equal(t, float64(valid.ID), snapshots[0]["id"])
	require.Equal(t, float64(supplier.ID), snapshots[0]["supplierID"])
	require.Equal(t, supplier.Code, snapshots[0]["supplierCode"])
	require.Equal(t, supplier.Name, snapshots[0]["supplierName"])
}
