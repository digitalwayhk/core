package public

import (
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type StatisticsResponse struct{}

type Statistics struct {
	api.ServerArgs
	FilterTypes []types.ApiType  `json:"filter_types"`
	SortBy      router.SortField `json:"sort_by"`
	Order       router.SortOrder `json:"order"`
}

func (own *Statistics) Parse(req types.IRequest) error {
	return req.Bind(own)
}

func (own *Statistics) Validation(req types.IRequest) error {
	return own.ServerArgs.Validation(req)
}

func (own *Statistics) Do(req types.IRequest) (interface{}, error) {
	items := make([]*router.AggregatedStats, 0)
	if own.ServiceName != "" {
		//sc := router.GetContext(own.ServiceName)
		// if sc != nil {
		// 	stat, err := own.getServiceStatistics(sc)
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// 	items = append(items, stat)
		// }
		// } else {
		// 	scs := router.GetContexts()
		// 	for _, sc := range scs {
		// 		stat, err := own.getServiceStatistics(sc)
		// 		if err != nil {
		// 			return nil, err
		// 		}
		// 		items = append(items, stat)
		// 	}
	}
	return items, nil
}

func (own *Statistics) getServiceStatistics(sc *router.ServiceContext) (*router.AggregatedStats, error) {
	//stats := sc.GetAllRouterStats(own.FilterTypes, own.SortBy, own.Order)
	return nil, nil
}

func (own *Statistics) RouterInfo() *types.RouterInfo {
	info := api.ServerRouterInfo(own)
	return info
}
