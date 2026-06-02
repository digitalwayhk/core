//go:build go1.20

package quic

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
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
	s1 := fmt.Sprintf("Starting %s server QUIC at %s:%d success\n", own.context.Config.Name, own.context.Config.RunIp, own.context.Config.Port+100)
	fmt.Print(s1)
	if err := own.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
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
