package run

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

//go:embed dist
var html embed.FS



type HTMLServer struct {
	Port     int
	services []*router.ServiceRouter
	Isstart  chan bool
	Parent   *WebServer
}

func NewHTMLServer(port int) *HTMLServer {
	ser := &HTMLServer{
		services: make([]*router.ServiceRouter, 0),
		Port:     port,
		Isstart:  make(chan bool, 1),
	}
	return ser
}
func (own *HTMLServer) AddServiceRouter(sr *router.ServiceRouter) {
	own.services = append(own.services, sr)
}

var qs = &public.QueryService{}

func (own *HTMLServer) Start() {
	if own.Port == 0 {
		return
	}
	run := <-own.Isstart
	if !run {
		return
	}
	sfsys, _ := fs.Sub(swagger, "swagger")
	http.Handle("/swagger/", http.StripPrefix("/swagger/", http.FileServer(http.FS(sfsys))))

	for _, service := range own.services {
		for _, router := range service.GetTypeRouters(types.ManageType) {
			http.Handle(router.Path+"/"+service.Service.Service.Name, htmlHandler(service))
		}
		for _, router := range service.GetTypeRouters(types.ServerManagerType) {
			if router.StructName != "QueryService" {
				http.Handle(router.Path+"/"+service.Service.Service.Name, htmlHandler(service))
			}
		}
	}
	http.Handle("/api/openapi", htmlHandler(own.services...))
	http.HandleFunc(qs.RouterInfo().Path, func(w http.ResponseWriter, r *http.Request) {
		data, _ := qs.Do(nil)
		httpx.OkJson(w, data)
	})
	pts := ""
	if own.Port != 80 {
		pts = ":" + strconv.Itoa(own.Port)
	}
	fmt.Println("===========================================================")
	var isview = true
	if own.Parent != nil {
		ops := own.Parent.GetServerOptions()
		for n, op := range ops {
			if op != nil && op.Demo != nil {
				if op.Demo.Pattern != "" {
					http.Handle("/"+op.Demo.Pattern+"/", http.StripPrefix("/"+op.Demo.Pattern+"/", http.FileServer(http.FS(op.Demo.File))))
				} else {
					http.Handle("/", http.FileServer(http.FS(op.Demo.File)))
					isview = false
				}
				fmt.Printf(n + "的Demo服务已经启动,请访问 http://localhost" + pts + "/" + op.Demo.Pattern + " 查看\n")
			}
		}
	}
	if isview {
		fsys, _ := fs.Sub(html, "dist")
		http.Handle("/", http.FileServer(http.FS(fsys)))
		fmt.Printf("开发视图服务已经启动,请访问 http://localhost" + pts + " 查看\n")
	}
	fmt.Println("===========================================================")
	http.ListenAndServe(":"+strconv.Itoa(own.Port), nil)
}
func (own *HTMLServer) Stop() {

}

func htmlHandler(service ...*router.ServiceRouter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url := strings.Trim(r.RequestURI, " ")
		if url == "/api/openapi" {
			httpx.OkJson(w, GetOpenApi(service...))
			return
		}
		if r.Method == "POST" {
			last := strings.LastIndex(url, "/")
			servicename := url[last+1:]
			index := strings.Index(servicename, "?")
			if index > 0 {
				servicename = servicename[:index]
			}
			path := url[:last]
			ss := getService(servicename, service)
			req := router.NewRequest(ss, r)
			req.SetPath(path)
			if item := ss.GetRouter(path); item != nil {
				res := item.Exec(req)
				httpx.OkJson(w, res)
			} else {
				httpx.OkJson(w, req.NewResponse(errors.New(req.GetPath()+"未找到对应的接口！"), nil))
			}
		}
	}
}
func getService(name string, ss []*router.ServiceRouter) *router.ServiceRouter {
	for _, s := range ss {
		if s.Service.Config.Name == name {
			return s
		}
	}
	return nil
}
