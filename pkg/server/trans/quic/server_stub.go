//go:build go1.20

package quic

import (
	"net/http"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/zeromicro/go-zero/core/logx"
)

type Server struct {
	server  *http.Server
	context *router.ServiceContext
}

func NewServer(context *router.ServiceContext) *Server {
	return &Server{
		server: &http.Server{
			Addr:    context.Config.RunIp + ":" + strconv.Itoa(context.Config.Port+100),
			Handler: setupHandler(context),
		},
		context: context,
	}
}

func (own *Server) Start() {
	logx.Infow("quic_compat_server_starting",
		logx.Field("service", own.context.Config.Name),
		logx.Field("port", own.context.Config.Port+100),
	)
	if err := own.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logx.Errorw("quic_compat_server_failed", logx.Field("error", err))
	}
}

func (own *Server) Stop() {
	if own.server != nil {
		_ = own.server.Close()
	}
}

func setupHandler(context *router.ServiceContext) http.Handler {
	mux := http.NewServeMux()
	for _, rou := range context.Router.GetRouters() {
		mux.HandleFunc(rou.Path, routeHandler(context.Router))
	}
	return mux
}

func routeHandler(sr *router.ServiceRouter) http.HandlerFunc {
	return rest.RouteHandler(sr)
}
