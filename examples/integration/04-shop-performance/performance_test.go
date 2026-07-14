package performanceshop_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteBehindRecoversOrderAfterProcessCrash(t *testing.T) {
	recoverySuite, err := startShopSuite()
	require.NoError(t, err)
	t.Cleanup(recoverySuite.Stop)

	admin := recoverySuite.TokenFor(t, "recovery-admin", 1)
	user := recoverySuite.TokenFor(t, "recovery-user", 0)
	product := recoverySuite.AddProduct(t, admin, fmt.Sprintf("恢复商品-%d", time.Now().UnixNano()), "25.00")
	order := recoverySuite.AddOrder(t, user, uintID(t, product.ID), 1)
	assert.Contains(t, orderIDs(recoverySuite.GetOrders(t, user)), order.ID, "下单返回后合并读必须立即可见")

	recoverySuite.KillProcess()
	require.NoError(t, recoverySuite.Restart())
	require.NoError(t, recoverySuite.waitReady())
	require.Eventually(t, func() bool {
		return containsOrderID(recoverySuite.GetOrders(t, user), order.ID)
	}, 5*time.Second, 50*time.Millisecond, "重启后必须恢复本地订单")

	recoverySuite.StopProcess()
	utils.TESTPATH = recoverySuite.RootDir
	action := oltp.NewSqlite()
	persisted, err := models.NewOrder().FindByIDWith(action, uintID(t, order.ID))
	require.NoError(t, err)
	require.NotNil(t, persisted, "优雅关闭前必须把恢复订单汇合到 SQLite")

	badgerPath := filepath.Join(recoverySuite.RootDir, "data", "order-write-behind")
	config := nosql.DefaultProductionConfig(badgerPath)
	config.EnableLogger = false
	db, err := nosql.NewSharedBadgerDB[models.Order](badgerPath, config)
	require.NoError(t, err)
	items, err := db.ScanAll()
	require.NoError(t, err)
	assert.Empty(t, items, "同步成功后的订单本地副本必须自动清理")
	require.NoError(t, db.Close())
}

func containsOrderID(items []OrderDTO, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
