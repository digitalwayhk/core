package public

import (
	"context"
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// RuntimeTopology 返回全局服务运行图拓扑。
type RuntimeTopology struct {
	api.ServerArgs
	Window string `json:"window"`
}

func (own *RuntimeTopology) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	if v := req.GetValue("window"); v != "" {
		own.Window = v
	}
	if own.Window == "" {
		own.Window = "15s"
	}
	return nil
}

func (own *RuntimeTopology) Validation(req types.IRequest) error {
	if err := own.ServerArgs.Validation(req); err != nil {
		return err
	}
	if _, ok := runtime.ParseWindow(own.Window); !ok {
		return errors.New("window must be one of: 15s, 5m, 1h")
	}
	return nil
}

func (own *RuntimeTopology) Do(req types.IRequest) (interface{}, error) {
	agg := aggregatorFromRequest(req)
	if agg == nil {
		return &runtime.TopologyResponse{
			GeneratedAt: time.Now().UTC(),
			Window:      own.Window,
			Status:      runtime.StateNotCollected,
			Services:    []runtime.ServiceNode{},
			Edges:       []runtime.ServiceEdge{},
			Warnings: []runtime.RuntimeWarning{{
				Code:    "aggregator_unavailable",
				Message: "runtime aggregator is not configured",
				Scope:   "global",
			}},
		}, nil
	}
	return agg.Topology(runtimeRequestContext(req), own.Window)
}

func (own *RuntimeTopology) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithPathType(types.ServerManagerType),
		withSystemEndpointRateLimit(),
	)
}

// RuntimeService 返回单个服务的请求聚合详情。
type RuntimeService struct {
	api.ServerArgs
	Window  string `json:"window"`
	Service string `json:"service"`
}

func (own *RuntimeService) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	if v := req.GetValue("window"); v != "" {
		own.Window = v
	}
	if v := req.GetValue("service"); v != "" {
		own.Service = v
	}
	if own.Window == "" {
		own.Window = "15s"
	}
	return nil
}

func (own *RuntimeService) Validation(req types.IRequest) error {
	if err := own.ServerArgs.Validation(req); err != nil {
		return err
	}
	if _, ok := runtime.ParseWindow(own.Window); !ok {
		return errors.New("window must be one of: 15s, 5m, 1h")
	}
	if own.Service == "" {
		return errors.New("service is required")
	}
	return nil
}

func (own *RuntimeService) Do(req types.IRequest) (interface{}, error) {
	agg := aggregatorFromRequest(req)
	if agg == nil {
		return nil, errors.New("runtime aggregator is not configured")
	}
	return agg.ServiceDetail(runtimeRequestContext(req), own.Window, own.Service)
}

func (own *RuntimeService) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithPathType(types.ServerManagerType),
		withSystemEndpointRateLimit(),
	)
}

func aggregatorFromRequest(req types.IRequest) *runtime.Aggregator {
	bound, ok := req.(interface {
		GetService() *router.ServiceContext
	})
	if !ok || bound.GetService() == nil {
		// 回退：从当前服务名取 ServiceContext。
		if sc := router.GetContext(req.ServiceName()); sc != nil {
			return sc.RuntimeAggregator
		}
		return nil
	}
	return bound.GetService().RuntimeAggregator
}

func runtimeRequestContext(req types.IRequest) context.Context {
	if httpReq, ok := req.(types.IRequestHttp); ok && httpReq.GetHttpRequest() != nil {
		return httpReq.GetHttpRequest().Context()
	}
	return context.Background()
}
