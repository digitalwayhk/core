// Package badgeropen 在Badger原打开入口前处理回收中断留下的空WAL；不重试、重建库或修改非空文件。
package badgeropen

import (
	"github.com/dgraph-io/badger/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

// Open 保留原Options和Badger独占锁，仅在可写且未绕锁时预处理已验证的零字节memtable。
func Open(opts badger.Options) (*badger.DB, error) {
	if !opts.InMemory && !opts.ReadOnly && !opts.BypassLockGuard {
		n, err := recoverEmptyMemtables(opts.Dir)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			logx.Infow("badger_empty_memtable_recovered", logx.Field("count", n))
		}
	}
	return badger.Open(opts)
}
