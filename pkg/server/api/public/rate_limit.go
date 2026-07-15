package public

import "github.com/digitalwayhk/core/pkg/server/router"

func withAuthEndpointRateLimit() router.RouterInfoOption {
	return router.WithExternalRateLimit(5, 10)
}

func withSystemEndpointRateLimit() router.RouterInfoOption {
	return router.WithExternalRateLimit(10, 20)
}

func withHealthEndpointRateLimit() router.RouterInfoOption {
	return router.WithExternalRateLimit(20, 40)
}
