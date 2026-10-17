// 本文件使用独立 etcd 客户端和持久事务保存编号权威。
package identity

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
	"time"
)

// EtcdStore 的编号键不挂租约，不随服务下线释放。
type EtcdStore struct {
	client *clientv3.Client
	key    string
}

// NewEtcdStore 创建编号专用客户端。
func NewEtcdStore(endpoints []string, key string) (*EtcdStore, error) {
	c, err := clientv3.New(clientv3.Config{Endpoints: endpoints, DialTimeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	return &EtcdStore{client: c, key: key}, nil
}
func (s *EtcdStore) Load(ctx context.Context) (string, error) {
	r, err := s.client.Get(ctx, s.key)
	if err != nil {
		return "", err
	}
	if len(r.Kvs) == 0 {
		return "", ErrAuthorityMissing
	}
	return string(r.Kvs[0].Value), nil
}
func (s *EtcdStore) CompareAndSwap(ctx context.Context, old, next string) (bool, error) {
	cmp := clientv3.Compare(clientv3.Value(s.key), "=", old)
	if old == "" {
		cmp = clientv3.Compare(clientv3.Version(s.key), "=", 0)
	}
	r, err := s.client.Txn(ctx).If(cmp).Then(clientv3.OpPut(s.key, next)).Commit()
	if err != nil {
		return false, err
	}
	return r.Succeeded, nil
}
func (s *EtcdStore) Close() error { return s.client.Close() }
