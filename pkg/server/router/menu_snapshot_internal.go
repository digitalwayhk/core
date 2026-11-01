// 本文件提供只读内部菜单快照协议，不注册HTTP路由或放宽ServerManage认证域。
package router

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/internal/menumetadata"
	"github.com/digitalwayhk/core/pkg/server/types"
)

func (sc *ServiceContext) handleInternalMenuSnapshot(ctx context.Context, payload *types.PayLoad) (bool, []byte, error) {
	if payload == nil || payload.TargetPath != menumetadata.Path {
		return false, nil, nil
	}
	caller, trusted := types.TrustedInternalCallerFromContext(ctx)
	if sc.Service == nil || !trusted || payload.SourceService != caller ||
		payload.HttpMethod != http.MethodPost || len(payload.Data) > 4096 {
		return true, nil, types.ErrInternalCallerForbidden
	}
	if err := sc.CheckServiceAdmission(); err != nil {
		return true, nil, err
	}
	var routes []*types.RouterInfo
	if sc.Router != nil {
		routes = sc.Router.GetTypeRouters(types.ManageType)
	}
	// 请求体不参与路由选择或筛选；只返回当前监听服务冻结的菜单元数据。
	data, err := json.Marshal(&Response{Success: true, ErrorCode: 200, TraceID: payload.TraceID,
		Data: menumetadata.New(sc.Service, routes)})
	return true, data, err
}
