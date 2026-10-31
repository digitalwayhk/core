// 本文件演示应用独立持久接管订单异常；接管只转移恢复责任，不执行补偿、不判定订单APPLIED。
package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

// OrderFailureReceipt 是应用独立保全的完整原订单、批次范围和接管回执，不引用待清理的Core原件。
type OrderFailureReceipt struct {
	Record        nosql.WriteBehindFailureRecord[transaction.Order]
	Unconfirmed   []nosql.WriteBehindFailureRecord[transaction.Order]
	ConfirmedKeys []string
	Receipt       nosql.WriteBehindHandoffReceipt
	State         string
}

// OrderFailureReceipts 由应用拥有独立Badger及恢复责任，关闭必须晚于Core store/在途回调退出。
type OrderFailureReceipts struct {
	db       *badger.DB
	path     string
	once     sync.Once
	closeErr error
}

// NewOrderFailureReceipts 打开应用自己的持久库；禁止与Core pending目录相同，不重置已有数据。
func NewOrderFailureReceipts(path string) (*OrderFailureReceipts, error) {
	if path == "" {
		return nil, errors.New("应用接管目录不能为空")
	}
	db, err := badger.Open(badger.DefaultOptions(path).WithSyncWrites(true).WithDetectConflicts(true).WithLogger(nil))
	if err != nil {
		return nil, err
	}
	return &OrderFailureReceipts{db: db, path: path}, nil
}

// OrderingGroup 由订单应用声明用户内顺序；Core不知道UserID字段或金融含义。
func (r *OrderFailureReceipts) OrderingGroup(v *nosql.SyncQueueItem[transaction.Order]) (string, error) {
	if v == nil || v.Item == nil || v.Item.UserID == 0 {
		return "", errors.New("订单顺序身份缺失")
	}
	return fmt.Sprintf("user:%d", v.Item.UserID), nil
}

// HandleFailure 是本示例显式选择的“立即持久接管”策略，不把网络错误/一次查无记录判为未提交。
// 生产应用须实现自己的结果查询、有限重试或恢复处理；本示例保留owned_unresolved及顺序屏障。
func (r *OrderFailureReceipts) HandleFailure(ctx context.Context, f nosql.WriteBehindFailure[transaction.Order]) (nosql.WriteBehindDecision, error) {
	if err := ctx.Err(); err != nil {
		return nosql.WriteBehindDecision{}, err
	}
	var saved OrderFailureReceipt
	err := r.db.Update(func(txn *badger.Txn) error {
		entry, err := txn.Get([]byte(f.Record.ID))
		if err == nil {
			return entry.Value(func(b []byte) error { return json.Unmarshal(b, &saved) })
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		saved = OrderFailureReceipt{Record: f.Record, Unconfirmed: f.Unconfirmed, ConfirmedKeys: f.ConfirmedKeys, State: "owned_unresolved", Receipt: nosql.WriteBehindHandoffReceipt{DispositionID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, ReceiptID: "order-exception:" + f.Record.ID, Owner: "shop-order-application", DurableReference: r.path + "#" + f.Record.ID}}
		b, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		return txn.Set([]byte(f.Record.ID), b)
	})
	if err != nil {
		return nosql.WriteBehindDecision{}, err
	}
	if err = ctx.Err(); err != nil {
		return nosql.WriteBehindDecision{}, err
	}
	return nosql.WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: nosql.WriteBehindHandoff, Scope: nosql.WriteBehindScopeGroup, Receipt: &saved.Receipt}, nil
}

// Read 供应用恢复流程按原身份读取独立保全事实，不提供匿名API或管理员页面。
func (r *OrderFailureReceipts) Read(id string) (OrderFailureReceipt, error) {
	var result OrderFailureReceipt
	err := r.db.View(func(txn *badger.Txn) error {
		entry, err := txn.Get([]byte(id))
		if err != nil {
			return err
		}
		return entry.Value(func(b []byte) error { return json.Unmarshal(b, &result) })
	})
	return result, err
}

// Close 幂等关闭应用接管库；不得在仍可能执行的回调持有此库时主动删除或回收目录。
func (r *OrderFailureReceipts) Close(context.Context) error {
	r.once.Do(func() { r.closeErr = r.db.Close() })
	return r.closeErr
}
