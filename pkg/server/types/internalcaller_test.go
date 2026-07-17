package types

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type internalCallerTestResponse struct {
	err error
}

func (r *internalCallerTestResponse) GetSuccess() bool                   { return r.err == nil }
func (r *internalCallerTestResponse) GetMessage() string                 { return "" }
func (r *internalCallerTestResponse) GetData(...interface{}) interface{} { return nil }
func (r *internalCallerTestResponse) GetError() error                    { return r.err }

type internalCallerTestRequest struct {
	shardTestRequest
	caller  string
	trusted bool
}

func (r *internalCallerTestRequest) NewResponse(_ interface{}, err error) IResponse {
	return &internalCallerTestResponse{err: err}
}

func (r *internalCallerTestRequest) TrustedInternalCaller() (string, bool) {
	return r.caller, r.trusted
}

var internalCallerRouteCounters struct {
	parse      atomic.Int32
	validation atomic.Int32
	do         atomic.Int32
}

type internalCallerCountingRouter struct{}

func (*internalCallerCountingRouter) Parse(IRequest) error {
	internalCallerRouteCounters.parse.Add(1)
	return nil
}

func (*internalCallerCountingRouter) Validation(IRequest) error {
	internalCallerRouteCounters.validation.Add(1)
	return nil
}

func (*internalCallerCountingRouter) Do(IRequest) (interface{}, error) {
	internalCallerRouteCounters.do.Add(1)
	return "ok", nil
}

func (*internalCallerCountingRouter) RouterInfo() *RouterInfo { return nil }

func newInternalCallerTestRouterInfo(t *testing.T) *RouterInfo {
	t.Helper()
	internalCallerRouteCounters.parse.Store(0)
	internalCallerRouteCounters.validation.Store(0)
	internalCallerRouteCounters.do.Store(0)
	info := &RouterInfo{
		Path:            "/api/test/internal",
		ServiceName:     "shop-order",
		Method:          "POST",
		PathType:        PublicType,
		InternalCallers: []string{"shop-user"},
	}
	info.SetInstance(&internalCallerCountingRouter{})
	info.Freeze("shop-order")
	return info
}

func TestConstrainedRouterRejectsUntrustedRequestBeforeParse(t *testing.T) {
	info := newInternalCallerTestRouterInfo(t)

	response := info.Exec(&internalCallerTestRequest{})

	require.ErrorIs(t, response.GetError(), ErrInternalCallerForbidden)
	require.Zero(t, internalCallerRouteCounters.parse.Load())
	require.Zero(t, internalCallerRouteCounters.validation.Load())
	require.Zero(t, internalCallerRouteCounters.do.Load())
}

func TestConstrainedRouterAcceptsAllowlistedTrustedRequest(t *testing.T) {
	info := newInternalCallerTestRouterInfo(t)

	response := info.Exec(&internalCallerTestRequest{caller: "shop-user", trusted: true})

	require.NoError(t, response.GetError())
	require.Equal(t, int32(1), internalCallerRouteCounters.parse.Load())
	require.Equal(t, int32(1), internalCallerRouteCounters.validation.Load())
	require.Equal(t, int32(1), internalCallerRouteCounters.do.Load())
}

func TestConstrainedRouterRejectsWrongTrustedServiceBeforeParse(t *testing.T) {
	info := newInternalCallerTestRouterInfo(t)

	response := info.Exec(&internalCallerTestRequest{caller: "shop-supplier", trusted: true})

	require.ErrorIs(t, response.GetError(), ErrInternalCallerForbidden)
	require.Zero(t, internalCallerRouteCounters.parse.Load())
	require.Zero(t, internalCallerRouteCounters.validation.Load())
	require.Zero(t, internalCallerRouteCounters.do.Load())
}
