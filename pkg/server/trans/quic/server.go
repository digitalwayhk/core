package quic

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/quic/testdata"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
)

type Server struct {
	server  *http3.Server
	context *router.ServiceContext
}

func NewServer(context *router.ServiceContext) *Server {
	return &Server{
		server: &http3.Server{
			Handler:    setupHandler(context),
			Addr:       context.Config.RunIp + ":" + strconv.Itoa(context.Config.Port+100),
			QuicConfig: &quic.Config{},
		},
		context: context,
	}
}
func (own *Server) Start() {
	s1 := fmt.Sprintf("Starting %s server QUIC at %s:%d success\n", own.context.Config.Name, own.context.Config.RunIp, own.context.Config.Port+100)
	fmt.Print(s1)
	err := own.server.ListenAndServeTLS(testdata.GetCertificatePaths())
	if err != nil {
		panic(err)
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
		mux.HandleFunc(rou.Path, routeHandler(context.Router))
	}
	return mux
}

func routeHandler(sr *router.ServiceRouter) http.HandlerFunc {
	return rest.RouteHandler(sr)
}
