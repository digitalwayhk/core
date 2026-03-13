package public

import (
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	"github.com/zeromicro/go-zero/core/logx"
)

type OpenAPI struct {
	api.ServerArgs
}

func (own *OpenAPI) Do(req types.IRequest) (interface{}, error) {
	serviceName := req.ServiceName()
	if serviceName == "" || serviceName == "server" {
		return nil, nil
	}
	sc := router.GetContext(req.ServiceName())
	if tih, ok := req.(types.IRequestHttp); ok {
		httpReq := tih.GetHttpRequest()
		return GetOpenApi(httpReq, sc.Router), nil
	}
	return nil, fmt.Errorf("invalid request type")
}

func (own *OpenAPI) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}

func GetOpenApi(req *http.Request, srs ...*router.ServiceRouter) interface{} {
	doc := &openapi3.T{}
	doc.OpenAPI = "3.0.1"
	doc.Info = &openapi3.Info{
		Title:       "Open API",
		Description: "Project API Document includ private and public",
		Version:     "1.0.0",
	}
	doc.Tags = make(openapi3.Tags, 0)
	doc.Servers = make(openapi3.Servers, 0)
	doc.Components = openapi3.NewComponents()
	doc.Components.Schemas = make(openapi3.Schemas, 0)

	host := req.Host
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	for _, r := range srs {
		if r.Service.Service.Name == "server" {
			continue
		}

		con := r.Service.Config
		var server *openapi3.Server
		tagdesc := ""
		if req.Header.Get("X-Forwarded-Proto") == "https" {
			server = &openapi3.Server{URL: "https://" + host + "/"}
		} else {
			server = &openapi3.Server{URL: "http://" + host + ":" + strconv.Itoa(con.Port) + "/"}
			tagdesc = server.URL
		}
		doc.Tags = append(doc.Tags, &openapi3.Tag{Name: r.Service.Service.Name, Description: tagdesc})
		isaddServer := true
		for _, s := range doc.Servers {
			if s.URL == server.URL {
				isaddServer = false
				break
			}
		}
		if isaddServer {
			doc.Servers = append(doc.Servers, server)
		}
		eachrouters(r.GetTypeRouters(types.PublicType), doc, server)
		eachrouters(r.GetTypeRouters(types.PrivateType), doc, server)
	}
	doc.Components.SecuritySchemes = make(openapi3.SecuritySchemes, 0)
	tokenURL := ""
	if len(doc.Servers) > 0 {
		tokenURL = doc.Servers[0].URL
	}
	doc.Components.SecuritySchemes["Bearer"] = &openapi3.SecuritySchemeRef{
		Value: &openapi3.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "Get TestToken from " + tokenURL + "api/servermanage/testtoken?userid=12345",
		},
	}
	return doc
}
func eachrouters(routers []*types.RouterInfo, doc *openapi3.T, server *openapi3.Server) {
	for _, r := range routers {
		path, method, oper := getOperation(r, doc)
		oper.Servers = &openapi3.Servers{server}
		doc.AddOperation(path, method, oper)
	}
}
func getOperation(info *types.RouterInfo, doc *openapi3.T) (path string, method string, operation *openapi3.Operation) {
	path = info.Path
	method = info.Method
	operation = &openapi3.Operation{
		Tags:        []string{info.ServiceName},
		Summary:     info.StructName,
		Responses:   make(openapi3.Responses, 0),
		OperationID: strings.TrimPrefix(strings.ReplaceAll(info.Path, "/", "_"), "_"),
	}
	api := info.New()
	defer func() {
		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:", info.ServiceName, info.Path), err)
			// 获取调用栈字符串并打印
			stack := debug.Stack()
			fmt.Printf("\nStack trace:\n%s\n", stack)
		}
	}()
	if method == "GET" {
		operation.Parameters = make(openapi3.Parameters, 0)
		utils.ForEach(api, func(name string, value interface{}) {
			operation.Parameters = append(operation.Parameters, &openapi3.ParameterRef{
				Value: &openapi3.Parameter{
					Name:        name,
					In:          "query",
					Schema:      &openapi3.SchemaRef{Value: &openapi3.Schema{Type: utils.GetTypeName(value)}},
					Description: getNameTag(api, name),
				},
			})
		})
	} else {
		operation.RequestBody = getRequestBody(api, doc)
	}
	req := &router.InitRequest{}
	data := router.TestResult[info.Path]
	if data == nil {
		if igp, ok := api.(types.IRouterResponse); ok {
			data = igp.GetResponse()
		}
	}
	ress := getResponse(data, req, doc)
	for k, v := range ress {
		operation.AddResponse(k, v)
	}
	if info.PathType == types.PrivateType {
		operation.Security = openapi3.NewSecurityRequirements()
		nsr := openapi3.NewSecurityRequirement()
		nsr.Authenticate("Bearer")
		operation.Security.With(nsr)
	}
	return
}

func getRequestBody(api interface{}, doc *openapi3.T) *openapi3.RequestBodyRef {
	ref := &openapi3.RequestBodyRef{}
	schema, _ := openapi3gen.NewSchemaRefForValue(api, nil, openapi3gen.UseAllExportedFields())
	if len(schema.Value.Properties) == 0 {
		return nil
	}
	//doc.Components.Schemas[utils.GetTypeName(api)] = schema
	body := openapi3.NewRequestBody()

	body.WithDescription(getTag(api))
	body.WithJSONSchema(schema.Value)
	body.WithRequired(true)
	ref.Value = body
	return ref
}
func getTag(api interface{}) string {
	desc := ""
	t := reflect.TypeOf(api)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("desc")
		if tag == "" {
			tag, _ = field.Tag.Lookup("desc")
		}
		name := field.Tag.Get("json")
		if name == "" {
			name = field.Name
		}
		if desc == "" {
			desc = name + ":" + tag
		} else {
			desc = desc + "<br>" + name + ":" + tag
		}
	}
	return desc
}
func getNameTag(api interface{}, name string) string {
	t := reflect.TypeOf(api)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if field, ok := t.FieldByName(name); ok {
		tag := field.Tag.Get("desc")
		if tag == "" {
			tag, _ = field.Tag.Lookup("desc")
		}
		name := field.Tag.Get("json")
		if name == "" {
			name = field.Name
		}
		return name + ":" + tag
	}
	return ""
}
func getResponse(data interface{}, req types.IRequest, doc *openapi3.T) map[int]*openapi3.Response {
	item := make(map[int]*openapi3.Response)
	res := req.NewResponse(data, nil)
	schema, _ := openapi3gen.NewSchemaRefForValue(res, nil, openapi3gen.UseAllExportedFields())
	schema.Value.Example = res
	content := openapi3.NewContentWithJSONSchema(schema.Value)
	msg := "Successful operation"
	opi3res := &openapi3.Response{Content: content, Description: &msg}
	item[200] = opi3res

	return item
}
