// 本文件以真实 MySQL 写入屏障制造 pending，验证硬崩溃及原卷显式恢复。
package shoporderscalemultiprocess

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func verifyStablePendingCrashRecovery(t *testing.T, authority *identity.Authority, addr, database string, user *integration.Suite, token string, productID uint, replicas []*integration.Suite) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	config := mysql.NewConfig()
	config.User, config.Passwd, config.Net, config.Addr, config.DBName = "root", os.Getenv("CORE_TEST_STABLE_MYSQL_PASSWORD"), "tcp", addr, database
	db, err := sql.Open("mysql", config.FormatDSN())
	require.NoError(t, err)
	defer db.Close()
	lock, err := db.Conn(ctx)
	require.NoError(t, err)
	defer lock.Close()
	// 只阻塞订单写入；幂等查询可继续，应用和框架均不增加故障注入后门。
	_, err = lock.ExecContext(ctx, "LOCK TABLES `order` READ")
	require.NoError(t, err)
	defer lock.ExecContext(context.Background(), "UNLOCK TABLES")
	const count = 8
	ids := make(map[uint]bool, count)
	requests := make(map[string]uint, count)
	for i := 0; i < count; i++ {
		requestID := fmt.Sprintf("crash-pending-%d", i)
		order := createDockerBuyerOrderWithRequest(t, user, token, productID, requestID)
		require.False(t, ids[order.OrderID], "新请求不可产生重复 ID")
		ids[order.OrderID] = true
		requests[requestID] = order.OrderID
	}
	var remote int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `order` WHERE request_id LIKE 'crash-pending-%'").Scan(&remote))
	require.Zero(t, remote, "硬杀前订单必须尚未进入远端，不能用正常重启替代积压恢复")
	for _, replica := range replicas {
		replica.KillProcess()
	}
	oldBindings := make([]*identity.Binding, len(replicas))
	pending := 0
	seen := make(map[uint]bool, count)
	for i, replica := range replicas {
		path := filepath.Join(replica.Environment["SHOP_IDENTITY_ROOT"], "shop-order", "identity.json")
		file, err := identity.Open(path, "shop-order", "")
		require.NoError(t, err, "硬杀并 wait 后原身份锁必须可独占取得")
		b, err := authority.Get(ctx, identity.InstanceKey("shop-order", file.State.InstanceID))
		require.NoError(t, err)
		require.False(t, b.Released)
		oldBindings[i] = b
		store, closeManager := openPendingCrashStore(t, replica, b)
		orders, err := store.PendingByUser(ctx, 720001)
		require.NoError(t, err)
		for _, order := range orders {
			if ids[order.ID] {
				require.Equal(t, requests[order.RequestID], order.ID)
				require.False(t, seen[order.ID])
				seen[order.ID] = true
				pending++
			}
		}
		// 读取只用于离线核对；Close 允许报告保留了未同步记录，不能吞掉其他关闭错误。
		pendingCount := store.Metrics().Pending
		closeErr := store.Close(ctx)
		if pendingCount > 0 || len(orders) > 0 {
			var pendingErr *nosql.PendingSyncError
			require.ErrorAs(t, closeErr, &pendingErr)
		} else {
			require.NoError(t, closeErr)
		}
		closeManager()
		require.NoError(t, file.Close())
		// 真实旧卷普通启动必须拒绝，不能仅根据 TTL 抢占旧 Boot。
		runCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		command := exec.CommandContext(runCtx, replica.Binary, replica.Arguments...)
		command.Dir = replica.RootDir
		command.Env = os.Environ()
		for key, value := range replica.Environment {
			command.Env = append(command.Env, key+"="+value)
		}
		output, runErr := command.CombinedOutput()
		require.NoError(t, runCtx.Err(), string(output))
		stop()
		require.Error(t, runErr, string(output))
		require.Contains(t, string(output), identity.ErrOwnerConflict.Error())
	}
	require.Equal(t, count, pending, "必须从原 Badger 卷读回每个已确认但未汇合订单")
	t.Logf("硬杀后：远端积压订单=%d，原卷读取 pending=%d；普通启动拒绝未释放 Boot", remote, pending)
	_, err = lock.ExecContext(ctx, "UNLOCK TABLES")
	require.NoError(t, err)
	for i, replica := range replicas {
		b := oldBindings[i]
		proof := identity.RecoveryProof{Key: b.Key, Epoch: "epoch-a", BootID: b.BootID, BootEpoch: b.BootEpoch, ReservedUntil: b.ReservedUntil, StoppedEvidence: "KillProcess 已 wait；身份锁与 Badger 原卷已独占打开并关闭", PendingEvidence: fmt.Sprintf("原路径 %s；全部副本合计核对 %d 条已确认 pending", replica.Environment["SHOP_LOCAL_PENDING_DIR"], pending)}
		require.NoError(t, authority.RecoverStoppedBoot(ctx, proof, true))
		require.NoError(t, authority.RecoverStoppedBoot(ctx, proof, false))
		require.NoError(t, replica.Restart())
		require.Eventually(t, func() bool { return nativeProbe(replica.BaseURL+"/api/ready") == http.StatusOK }, 30*time.Second, 100*time.Millisecond)
		after, err := authority.Get(ctx, b.Key)
		require.NoError(t, err)
		require.Equal(t, b.InstanceID, after.InstanceID)
		require.Equal(t, b.WorkerID, after.WorkerID)
		require.Equal(t, b.MachineID, after.MachineID)
		require.Greater(t, after.BootEpoch, b.BootEpoch)
		require.NotEqual(t, b.BootID, after.BootID)
		require.GreaterOrEqual(t, after.ReservedUntil, b.ReservedUntil)
	}
	require.Eventually(t, func() bool {
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `order` WHERE request_id LIKE 'crash-pending-%'").Scan(&remote)
		return err == nil && remote == count
	}, 30*time.Second, 100*time.Millisecond)
	var outboxes int
	require.Eventually(t, func() bool {
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox_record WHERE published = 1").Scan(&outboxes)
		return err == nil && outboxes == count+1
	}, 30*time.Second, 100*time.Millisecond)
	for i := 0; i < count; i++ {
		requestID := fmt.Sprintf("crash-pending-%d", i)
		// 先用真实跨服务查询确认连接恢复；本进程 ready 不代表调用方的 gRPC 连接已重连。
		waitDockerOrderVisible(t, user, token, requests[requestID])
		retried := createDockerBuyerOrderWithRequest(t, user, token, productID, requestID)
		require.Equal(t, requests[requestID], retried.OrderID, "恢复后同一请求必须返回其原订单 ID")
		waitDockerOrderVisible(t, user, token, retried.OrderID)
	}
	for i, replica := range replicas {
		replica.StopProcess()
		b := oldBindings[i]
		store, closeManager := openPendingCrashStore(t, replica, b)
		require.Zero(t, store.Metrics().Pending, "远端与 Outbox 完成后本地 pending 应 ACK 清空")
		require.NoError(t, store.Close(ctx))
		closeManager()
	}
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `order` WHERE request_id LIKE 'crash-pending-%'").Scan(&remote))
	require.Equal(t, count, remote)
	t.Logf("恢复后：%d 条远端订单、%d 条已发布 Outbox（含原用例1条），幂等 ID 保持，原卷 pending=0", remote, outboxes)
}

func openPendingCrashStore(t *testing.T, replica *integration.Suite, binding *identity.Binding) (*transaction.OrderWriteStore, func()) {
	t.Helper()
	base := replica.Environment["SHOP_LOCAL_PENDING_DIR"]
	badger := nosql.DefaultProductionConfig(base)
	badger.AutoSync, badger.EnableLogger = false, false
	store, err := transaction.NewOrderWriteStore(nosql.ServiceIdentity{ServiceName: "shop-order", DataCenterID: int64(binding.DataCenterID), MachineID: int64(binding.MachineID)}, nosql.ReliableWriteStoreConfig{BasePath: base, Badger: badger})
	require.NoError(t, err)
	path := filepath.Join(base, "shop-order", fmt.Sprintf("dc-%d", binding.DataCenterID), fmt.Sprintf("machine-%d", binding.MachineID))
	closeManager := func() { require.NoError(t, nosql.CloseSharedManager(path)) }
	t.Cleanup(func() { _ = store.Close(context.Background()); closeManager() })
	return store, closeManager
}
