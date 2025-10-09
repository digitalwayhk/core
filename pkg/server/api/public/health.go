package public

import (
	"net/http"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Health struct {
	Status string `json:"status"`
}
type HealthResponse struct {
	Status string `json:"status"`
	//Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
}

func (own *Health) Parse(req types.IRequest) error {
	return nil
}

func (own *Health) Validation(req types.IRequest) error {
	return nil
}

func (own *Health) Do(req types.IRequest) (interface{}, error) {
	health := &HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	return health, nil
}

func (own *Health) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = http.MethodGet
	info.Path = "/api/health"
	return info
}
