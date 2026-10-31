// 本文件按已恢复的持续归属精确路由，不使用健康哈希候选替代owner。
package router

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
	"time"
)

// CallServiceOwned 跟随同scope/key的Ready owner，未知结果不自动重试。
func (sc *ServiceContext) CallServiceOwned(ctx context.Context, payload *types.PayLoad, scope, key string) (types.IResponse, error) {
	if sc == nil || sc.MQManager == nil || sc.Service == nil || payload == nil {
		return nil, ownership.ErrUnsupported
	}
	store, err := sc.MQManager.OwnershipStore(ctx)
	if err != nil {
		return nil, err
	}
	credential, err := store.Resolve(ctx, scope, key)
	if err != nil {
		return nil, err
	}
	if credential.Service != payload.TargetService {
		return nil, ownership.ErrFenced
	}
	copyPayload := *payload
	copyPayload.ServiceHashKey = ""
	copyPayload.TargetInstanceID = credential.InstanceID
	copyPayload.SourceService = sc.Service.Name
	ctx = types.ContextWithTrustedInternalCaller(ctx, sc.Service.Name)
	start := time.Now()
	data, protocol, err := sc.invokePayload(ctx, &copyPayload)
	sc.recordServiceCall(&copyPayload, protocol, err, time.Since(start))
	if err != nil {
		return nil, err
	}
	// 归属在执行期间变化时不能把旧本地缓存/控制结果当有效结果返回，也不自动重试未知副作用。
	if err := store.Validate(ctx, credential, true); err != nil {
		return nil, err
	}
	response := &Response{}
	if err := json.Unmarshal(data, response); err != nil {
		return nil, err
	}
	return response, nil
}
