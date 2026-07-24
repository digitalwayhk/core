//go:build !go1.20
// +build !go1.20

package quic

import (
	"net/http"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/quic/testdata"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"github.com/zeromicro/go-zero/core/logx"
)

type Server struct {
	server  *http3.Server
	context *router.ServiceContext
}

func NewServer(context *router.ServiceContext) *Server {
	return &Server{
		server: &http3.Server{
			Handler:    setupHandler(context),
			Addr:       context.RuntimeAddress() + ":" + strconv.Itoa(context.Config.Port+100),
			QuicConfig: &quic.Config{},
		},
		context: context,
	}
}
func (own *Server) Start() {
	logx.Infow("quic_server_starting",
		logx.Field("service", own.context.Config.Name),
		logx.Field("port", own.context.Config.Port+100),
	)
	if err := own.server.ListenAndServeTLS(testdata.GetCertificatePaths()); err != nil {
		logx.Errorw("quic_server_failed", logx.Field("error", err))
	}
}
func (own *Server) Stop() {
	if own.server != nil {
		own.server.Close()
	}
}

func setupHandler(context *router.ServiceContext) http.Handler {
	mux := http.NewServeMux()
	for _, rou := range context.Router.GetRouters() {
		mux.HandleFunc(rou.GetPath(), routeHandler(context.Router))
	}
	return mux
}

func routeHandler(sr *router.ServiceRouter) http.HandlerFunc {
	return rest.RouteHandler(sr)
}
