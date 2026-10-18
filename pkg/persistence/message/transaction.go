// Package message 提供复用业务数据库的消息事务工具，不声明业务模型、表或投递目标。
package message

import (
	"context"
	"errors"
	"reflect"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"gorm.io/gorm"
)

// Run 在独立 action 上提交业务事实与消息依据；错误、取消和 panic 均回滚，不自动重放写入。
// 建表须由调用方启动阶段完成；同一个 action 不能被多个 goroutine 并发使用。
// ctx 在执行前及提交前检查；IDataAction 操作内部的中断/超时仍由适配器或业务回调负责。
func Run(ctx context.Context, base types.IDataAction, operation func(types.IDataAction) error) (err error) {
	if ctx == nil || nilValue(base) || operation == nil {
		return errors.New("消息事务参数不完整")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cloner, ok := base.(interface{ Clone() types.IDataAction })
	if !ok {
		return errors.New("数据操作器不支持独立事务")
	}
	action := cloner.Clone()
	if nilValue(action) {
		return errors.New("数据操作器克隆为空")
	}
	if err = action.Transaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, action.Rollback())
		}
	}()
	if err = operation(action); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = action.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// transactionDB 只取得已显式开启的调用方事务；不能用 GetRunDB 回退到基础连接。
func transactionDB(ctx context.Context, action types.IDataAction, model interface{}) (*gorm.DB, error) {
	if ctx == nil || nilValue(action) || nilValue(model) {
		return nil, errors.New("消息事务参数不完整")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	access, ok := action.(types.ITransactionDB)
	if !ok {
		return nil, types.ErrTransactionDBUnavailable
	}
	// 模型访问只完成库选择与懒事务初始化，不负责建表。
	if _, err := action.GetModelDB(model); err != nil {
		return nil, err
	}
	db, err := access.GetTransactionDB()
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, types.ErrTransactionDBUnavailable
	}
	return db.WithContext(ctx), nil
}

func nilValue(value interface{}) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
