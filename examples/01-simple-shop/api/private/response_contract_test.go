package private

import servertypes "github.com/digitalwayhk/core/pkg/server/types"

var _ servertypes.IRouterResponse = (*AddOrder)(nil)
var _ servertypes.IRouterResponse = (*GetOrders)(nil)
var _ servertypes.IRouterResponse = (*DeleteOrder)(nil)
