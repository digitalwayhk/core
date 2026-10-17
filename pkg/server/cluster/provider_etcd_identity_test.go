// 本文件验证 etcd 按准确实例查询，不能误取同服务的第一个节点。
package cluster

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type identityEtcdKV struct {
	clientv3.KV
	response *clientv3.GetResponse
}

func (k identityEtcdKV) Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return k.response, nil
}

// TestEtcdGetMatchesExactNodeID 验证多实例查询不串实例。
func TestEtcdGetMatchesExactNodeID(t *testing.T) {
	first, _ := json.Marshal(&NodeInfo{ID: "first", ServiceName: "orders"})
	second, _ := json.Marshal(&NodeInfo{ID: "second", ServiceName: "orders"})
	p := &EtcdProvider{prefix: "/test", get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{{Key: []byte("/test/orders/first"), Value: first}, {Key: []byte("/test/orders/second"), Value: second}}}, nil
	}}
	p.client = &clientv3.Client{KV: identityEtcdKV{response: func() *clientv3.GetResponse { r, _ := p.get(context.Background(), ""); return r }()}}
	n, err := p.Get(context.Background(), "second")
	require.NoError(t, err)
	require.Equal(t, "second", n.ID)
	_, err = p.Get(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNodeNotFound)
}
