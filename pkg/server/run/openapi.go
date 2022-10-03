package run

import (
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
)

func GetOpenApi(srs ...*router.ServiceRouter) interface{} {
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
	doc.Components.SecuritySchemes = make(openapi3.SecuritySchemes, 0)
	doc.Components.SecuritySchemes["Bearer"] = &openapi3.SecuritySchemeRef{
		Value: &openapi3.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Flows: &openapi3.OAuthFlows{
				Implicit: &openapi3.OAuthFlow{
					AuthorizationURL: "http://localhost:8080/api/servermanage/testtoken",
					Scopes: map[string]string{
						"read":  "read access",
						"write": "write access",
					},
				},
			},
		},
	}
	for _, r := range srs {
		if r.Service.Service.Name == "server" {
			continue
		}
		doc.Tags = append(doc.Tags, &openapi3.Tag{Name: r.Service.Service.Name})
		con := r.Service.Config
		server := &openapi3.Server{URL: "http://" + con.RunIp + ":" + strconv.Itoa(con.Port) + "/"}
		doc.Servers = append(doc.Servers, server)
		eachrouters(r.GetTypeRouters(types.PublicType), doc, server)
		eachrouters(r.GetTypeRouters(types.PrivateType), doc, server)
	}
	return doc
}
func eachrouters(routers []*types.RouterInfo, doc *openapi3.T, server *openapi3.Server) {
	for _, r := range routers {
		oper := getrouter(r, doc, server)
		oper.Servers = &openapi3.Servers{server}
		doc.AddOperation(r.Path, r.Method, oper)
	}
}
func getrouter(info *types.RouterInfo, doc *openapi3.T, server *openapi3.Server) *openapi3.Operation {
	oper := &openapi3.Operation{
		Tags:        []string{info.ServiceName},
		Description: info.StructName,
		Responses:   make(openapi3.Responses, 0),
		OperationID: info.Path,
	}
	api := info.New()

	oper.RequestBody = getRequestBody(api, doc)
	req := &router.InitRequest{}
	data, _ := safedo(api, req)
	ress := getResponse(data, req, doc)
	for k, v := range ress {
		oper.AddResponse(k, v)
	}
	if info.PathType == types.PrivateType {
		oper.Security = openapi3.NewSecurityRequirements()
		nsr := openapi3.NewSecurityRequirement()
		nsr.Authenticate(server.URL+"/api/servermanage/testtoken ", "Bearer", "userid")
		oper.Security.With(nsr)
	}
	return oper
}
func getRequestBody(api interface{}, doc *openapi3.T) *openapi3.RequestBodyRef {
	ref := &openapi3.RequestBodyRef{}
	schema, _ := openapi3gen.NewSchemaRefForValue(api, nil, openapi3gen.UseAllExportedFields())
	if len(schema.Value.Properties) == 0 {
		return nil
	}
	doc.Components.Schemas[utils.GetTypeName(api)] = schema
	body := openapi3.NewRequestBody()
	body.WithDescription("request body")
	body.WithJSONSchema(schema.Value)
	body.WithRequired(true)
	ref.Value = body
	return ref
}

func getResponse(data interface{}, req types.IRequest, doc *openapi3.T) map[int]*openapi3.Response {
	item := make(map[int]*openapi3.Response)
	res := req.NewResponse(data, nil)
	schema, _ := openapi3gen.NewSchemaRefForValue(res, nil, openapi3gen.UseAllExportedFields())
	schema.Value.Example = res
	content := openapi3.NewContentWithJSONSchema(schema.Value)
	msg := "Successful operation"
	item[200] = &openapi3.Response{Content: content, Description: &msg}
	if data != nil {
		doc.Components.Schemas[utils.GetTypeName(data)] = schema
	}
	doc.Components.Schemas[utils.GetTypeName(res)] = schema
	errres := &router.Response{
		ErrorCode:    600,
		ErrorMessage: "参数解析异常----Parse return error",
	}
	err600, _ := openapi3gen.NewSchemaRefForValue(errres, nil, openapi3gen.UseAllExportedFields())
	err600.Value.Example = errres
	item[600] = &openapi3.Response{Content: openapi3.NewContentWithJSONSchema(err600.Value), Description: &errres.ErrorMessage}
	errres700 := &router.Response{
		ErrorCode:    700,
		ErrorMessage: "业务验证异常----Validation return error",
	}
	item[700] = &openapi3.Response{Description: &errres700.ErrorMessage}
	errres800 := &router.Response{
		ErrorCode:    800,
		ErrorMessage: "调用执行异常----Do return error",
	}
	item[800] = &openapi3.Response{Description: &errres800.ErrorMessage}
	return item
}
func safedo(cs types.IRouter, req types.IRequest) (interface{}, error) {
	defer func() {
		if err := recover(); err != nil {
			//fmt.Println("捕获异常:", err)
		}
	}()
	// load := &types.PayLoad{
	// 	TraceID:       "123456",
	// 	UserId:        123456,
	// 	UserName:      "123456",
	// 	TargetService: info.ServiceName,
	// 	TargetPath:    info.Path,
	// }
	// req := router.ToRequest(load)
	return cs.Do(req)
}
