package run

import (
	"embed"
	"io/fs"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/internal/openapiutil"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
)

//go:embed swagger
var swagger embed.FS

func SwaggerHandler() (string, http.FileSystem) {
	sfsys, _ := fs.Sub(swagger, "swagger")
	return "/swagger/", http.FS(sfsys)
}
func OpenAPIHandler(service ...*router.ServiceRouter) (string, http.Handler) {
	return "/api/openapi", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.OkJson(w, GetOpenApi(r, service...))
	})
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
	components := openapi3.NewComponents()
	doc.Components = &components
	doc.Components.Schemas = make(openapi3.Schemas, 0)
	doc.Paths = openapi3.NewPaths()

	host := req.Host
	if strings.Index(host, ":") > 0 {
		host = host[:strings.Index(host, ":")]
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
	bearerDescription := "Bearer token authentication"
	if len(doc.Servers) > 0 {
		bearerDescription = "Get TestToken from " + doc.Servers[0].URL + "api/servermanage/testtoken?userid=12345"
	}
	doc.Components.SecuritySchemes["Bearer"] = &openapi3.SecuritySchemeRef{
		Value: &openapi3.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  bearerDescription,
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
	path = info.GetPath()
	method = info.GetMethod()
	operation = &openapi3.Operation{
		Tags: []string{info.GetServiceName()},
		//Description: strings.ToUpper(info.StructName),
		Responses:   openapi3.NewResponsesWithCapacity(1),
		OperationID: info.GetPath(),
	}
	if callers := info.GetInternalCallers(); len(callers) > 0 {
		operation.Extensions = map[string]interface{}{
			"x-internal-callers": callers,
		}
	}
	api := info.New()
	defer func() {
		if err := recover(); err != nil {
			logx.Errorw("openapi_router_panicked",
				logx.Field("service", info.GetServiceName()),
				logx.Field("route", info.GetPath()),
				logx.Field("error", err),
				logx.Field("stack", string(debug.Stack())),
			)
		}
	}()
	if method == "GET" {
		operation.Parameters = make(openapi3.Parameters, 0)
		utils.ForEach(api, func(name string, value interface{}) {
			operation.Parameters = append(operation.Parameters, &openapi3.ParameterRef{
				Value: &openapi3.Parameter{
					Name:        name,
					In:          "query",
					Schema:      openapiutil.SchemaRefForValue(value),
					Description: getNameTag(api, name),
				},
			})
		})
	} else {
		operation.RequestBody = getRequestBody(api, doc)
	}
	req := &router.InitRequest{}
	data := router.GetTestResult(info.GetPath())
	if data == nil {
		if igp, ok := api.(types.IRouterResponse); ok {
			data = igp.GetResponse()
		}
	}
	ress := getResponse(data, req, doc)
	for k, v := range ress {
		operation.AddResponse(k, v)
	}
	if info.GetPathType() == types.PrivateType {
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
			desc = desc + "</br>" + name + ":" + tag
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
	//opi3res.WithJSONSchema(schema.Value)
	item[200] = opi3res
	// if data != nil {
	// 	doc.Components.Schemas[utils.GetTypeName(data)] = schema
	// }
	// doc.Components.Schemas[utils.GetTypeName(res)] = schema

	// errres := &router.Response{
	// 	ErrorCode:    600,
	// 	ErrorMessage: "参数解析异常----Parse return error",
	// }
	// err600, _ := openapi3gen.NewSchemaRefForValue(errres, nil, openapi3gen.UseAllExportedFields())
	// err600.Value.Example = errres
	// item[600] = &openapi3.Response{Content: openapi3.NewContentWithJSONSchema(err600.Value), Description: &errres.ErrorMessage}
	// errres700 := &router.Response{
	// 	ErrorCode:    700,
	// 	ErrorMessage: "业务验证异常----Validation return error",
	// }
	// item[700] = &openapi3.Response{Description: &errres700.ErrorMessage}
	// errres800 := &router.Response{
	// 	ErrorCode:    800,
	// 	ErrorMessage: "调用执行异常----Do return error",
	// }
	// item[800] = &openapi3.Response{Description: &errres800.ErrorMessage}
	return item
}
