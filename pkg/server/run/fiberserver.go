package run

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type FiberServer struct {
	Port     int
	services []*router.ServiceRouter
	isstart  chan bool
	Parent   *WebServer
}

func NewFiberServer(port int) *FiberServer {
	ser := &FiberServer{
		services: make([]*router.ServiceRouter, 0),
		Port:     port,
		isstart:  make(chan bool, 1),
	}
	return ser
}
func (own *FiberServer) Run() {
	own.isstart <- true
}
func (own *FiberServer) AddServiceRouter(sr *router.ServiceRouter) {
	own.services = append(own.services, sr)
}

func (own *FiberServer) Start() {
	if own.Port <= 0 {
		return
	}
	run := <-own.isstart
	if !run {
		return
	}
	app := fiber.New()
	fsys, _ := fs.Sub(html, "dist")
	app.Use("/", filesystem.New(filesystem.Config{
		Root: http.FS(fsys),
	}))
	openapi, ohandler := OpenAPIHandler()
	app.Post(openapi, adaptor.HTTPHandler(ohandler))
	app.Post("/api/*", adaptor.HTTPHandler(handler(own.services)))
	swagger, shandler := SwaggerHandler()
	app.Use("/"+swagger, filesystem.New(filesystem.Config{
		Root: shandler,
	}))
	app.Listen(":" + strconv.Itoa(own.Port))
}
func (own *FiberServer) Stop() {

}

func handler(services []*router.ServiceRouter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		url := strings.Trim(r.RequestURI, " ")
		_, path := getServiceAndPath(url)
		for _, service := range services {
			info := service.GetRouter(path)
			if info != nil {
				req := router.NewRequest(service, r)
				req.SetPath(path)
				res := info.Exec(req)
				httpx.OkJson(w, res)
				return
			}
		}
		req := router.NewRequest(nil, r)
		httpx.OkJson(w, req.NewResponse(errors.New(path+"未找到对应的接口！"), nil))
	})
}
func getServiceAndPath(url string) (string, string) {
	last := strings.LastIndex(url, "/")
	servicename := url[last+1:]
	index := strings.Index(servicename, "?")
	if index > 0 {
		servicename = servicename[:index]
	}
	path := url[:last]
	return servicename, path
}
