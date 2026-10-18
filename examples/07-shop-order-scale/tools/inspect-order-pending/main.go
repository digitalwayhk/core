// 本工具只读检查已关闭的Badger副本；不修复、不重建、不删除任何事实或索引。
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dgraph-io/badger/v3"
)

func main() {
	path := flag.String("path", "", "已关闭数据库的副本目录")
	format := flag.String("format", "", "数据格式：native或core")
	prefix := flag.String("prefix", "", "Core模型物理前缀，例如Order:")
	flag.Parse()
	report, err := inspectPending(*path, *format, *prefix)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func inspectPending(path, format, prefix string) (report inspectionReport, err error) {
	report = inspectionReport{Format: format, ReadOnly: true, Issues: make(map[string]int64)}
	if format != "native" && format != "core" {
		return report, errors.New("format必须为native或core")
	}
	if format == "core" && prefix == "" {
		return report, errors.New("Core核对必须指定模型prefix")
	}
	info, err := os.Stat(path)
	if err != nil {
		return report, err
	}
	if !info.IsDir() {
		return report, errors.New("path必须是已关闭数据库的副本目录")
	}
	db, err := badger.Open(badger.DefaultOptions(path).WithReadOnly(true).WithLogger(nil))
	if err != nil {
		return report, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	err = db.View(func(tx *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.PrefetchValues = false
		it := tx.NewIterator(options)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			factPrefix, queuePrefix := "p:", "q:"
			if format == "core" {
				factPrefix, queuePrefix = prefix, "\x00sq:"+prefix
			}
			isFact, isQueue := bytes.HasPrefix(key, []byte(factPrefix)), bytes.HasPrefix(key, []byte(queuePrefix))
			if !isFact && !isQueue {
				continue
			}
			if isFact {
				report.Facts++
			}
			if isQueue {
				report.Queues++
			}
			value, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			if format == "native" {
				if isFact {
					report.Unsynced++
					e = inspectNativeFact(tx, key, value, &report)
				} else {
					fact, getErr := tx.Get(value)
					if errors.Is(getErr, badger.ErrKeyNotFound) {
						report.Issues["queue_without_fact"]++
						continue
					}
					if getErr != nil {
						return getErr
					}
					data, getErr := fact.ValueCopy(nil)
					if getErr != nil {
						return getErr
					}
					p, q, decodeErr := nativeKeys(data)
					if decodeErr != nil || !bytes.Equal(p, value) || !bytes.Equal(q, key) {
						report.Issues["queue_identity_mismatch"]++
					}
				}
			} else if isFact {
				e = inspectCoreFact(tx, key, value, &report)
			} else {
				dataKey := strings.TrimPrefix(string(key), "\x00sq:")
				fact, getErr := tx.Get([]byte(dataKey))
				if errors.Is(getErr, badger.ErrKeyNotFound) {
					report.Issues["queue_without_fact"]++
					continue
				}
				if getErr != nil {
					return getErr
				}
				var state struct {
					IsSynced *bool `json:"is_synced"`
				}
				getErr = fact.Value(func(v []byte) error { return json.Unmarshal(v, &state) })
				if getErr != nil || state.IsSynced == nil || *state.IsSynced {
					report.Issues["queue_without_pending"]++
				}
			}
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err == nil && len(report.Issues) != 0 {
		err = errors.New("物理事实和队列索引不一致，详见issues；未执行修复")
	}
	return report, err
}

func nativeKeys(data []byte) ([]byte, []byte, error) {
	var order struct {
		ID          uint64 `json:"orderID"`
		UserID      uint64 `json:"userID"`
		RequestID   string `json:"requestID"`
		Fingerprint string `json:"requestFingerprint"`
	}
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, nil, err
	}
	if order.ID == 0 || order.UserID == 0 || order.RequestID == "" || order.Fingerprint == "" {
		return nil, nil, errors.New("事实缺失身份")
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", order.UserID, order.RequestID)))
	return []byte(fmt.Sprintf("p:%x", hash)), []byte(fmt.Sprintf("q:%020d", order.ID)), nil
}

func inspectNativeFact(tx *badger.Txn, key, data []byte, report *inspectionReport) error {
	p, q, err := nativeKeys(data)
	if err != nil {
		report.Issues["invalid_fact"]++
		return nil
	}
	if !bytes.Equal(p, key) {
		report.Issues["fact_identity_mismatch"]++
	}
	item, err := tx.Get(q)
	if errors.Is(err, badger.ErrKeyNotFound) {
		report.Issues["fact_without_queue"]++
		return nil
	}
	if err != nil {
		return err
	}
	value, err := item.ValueCopy(nil)
	if err == nil && !bytes.Equal(value, key) {
		report.Issues["fact_queue_mismatch"]++
	}
	return err
}

func inspectCoreFact(tx *badger.Txn, key, data []byte, report *inspectionReport) error {
	var wrapper struct {
		Key       string          `json:"key"`
		IsSynced  *bool           `json:"is_synced"`
		IsDeleted bool            `json:"is_deleted"`
		Item      json.RawMessage `json:"item"`
	}
	if json.Unmarshal(data, &wrapper) != nil || wrapper.IsSynced == nil || wrapper.Key != string(key) ||
		(!wrapper.IsDeleted && !bytes.HasPrefix(bytes.TrimSpace(wrapper.Item), []byte("{"))) {
		report.Issues["invalid_fact"]++
		return nil
	}
	if *wrapper.IsSynced {
		return nil
	}
	report.Unsynced++
	_, err := tx.Get(append([]byte("\x00sq:"), key...))
	if errors.Is(err, badger.ErrKeyNotFound) {
		report.Issues["fact_without_queue"]++
		return nil
	}
	return err
}
