// 本文件在显式指定的专用 MySQL 中验证并发消费事务，默认不连接任何外部数据库。
package message_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/message"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestMySQLConcurrentInboxCommitThenReplay 验证两个独立事务消费同一事件，重投后只保留一次业务变更。
func TestMySQLConcurrentInboxCommitThenReplay(t *testing.T) {
	address := os.Getenv("CORE_MESSAGE_MYSQL_ADDR")
	if address == "" {
		t.Skip("需要显式专用 CORE_MESSAGE_MYSQL_ADDR")
	}
	host, portString, err := net.SplitHostPort(address)
	require.NoError(t, err)
	port, err := strconv.Atoi(portString)
	require.NoError(t, err)
	config := &oltp.Config{Host: host, Port: port, Username: "root", Password: os.Getenv("CORE_MESSAGE_MYSQL_PASSWORD"), Database: fmt.Sprintf("core_message_%d", time.Now().UnixNano()), Charset: "utf8mb4", ParseTime: true, Loc: "UTC", MaxOpenConns: 8, MaxIdleConns: 2}
	base := oltp.NewMySQL(config)
	var rows []*messageFact
	require.NoError(t, base.Load(&types.SearchItem{Model: newFact(""), Page: 1, Size: 1, SkipCount: true}, &rows))
	counter := newFact("inventory-counter")
	require.NoError(t, base.Insert(counter))
	consume := func() error {
		receipt := newFact("inventory-consumer:event-1")
		query := &types.SearchItem{}
		query.AddWhereN("EventID", receipt.EventID)
		return message.Run(context.Background(), base, func(tx types.IDataAction) error {
			return message.ConsumeInboxWith(context.Background(), tx, receipt, query, func(tx types.IDataAction) error {
				db, err := tx.(types.ITransactionDB).GetTransactionDB()
				if err != nil {
					return err
				}
				return db.Model(newFact("")).Where("id = ?", counter.ID).UpdateColumn("value", gorm.Expr("value + ?", 1)).Error
			})
		})
	}
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errors <- consume() }()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		// 唯一约束/死锁使竞争事务回滚；MQ 可在新事务重投，框架不自动重试未知写入。
		if err != nil {
			require.NoError(t, consume())
		}
	}
	// 模拟业务提交后 ACK 丢失：新 action 仍从数据库识别已处理记录。
	base = oltp.NewMySQL(config)
	require.NoError(t, consume())
	rows = readFacts(t, base)
	require.Len(t, rows, 2)
	for _, row := range rows {
		if row.ID == counter.ID {
			require.Equal(t, 1, row.Value)
		}
	}
}
