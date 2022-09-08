package run

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
)

func GetOpenApi(srs ...*router.ServiceRouter) interface{} {
	doc := &openapi3.T{}
	doc.OpenAPI = "3.0.1"
	doc.Info = &openapi3.Info{
		Title:       "Crypto",
		Description: "Crypto SPOT Project",
		Version:     "1.0.0",
	}
	doc.Servers = make(openapi3.Servers, 0)
	for _, r := range srs {
		con := r.Service.Config
		doc.Servers = append(doc.Servers, &openapi3.Server{URL: "http://" + con.RunIp + ":" + strconv.Itoa(con.Port) + "/"})
		eachrouters(r.GetTypeRouters(types.PublicType), doc)
		eachrouters(r.GetTypeRouters(types.PrivateType), doc)
	}
	return doc
}
func eachrouters(routers []*types.RouterInfo, doc *openapi3.T) {
	for _, r := range routers {
		index := strings.LastIndex(r.Path, "/")
		oper := &openapi3.Operation{
			Tags:        []string{r.ServiceName + "-" + string(r.PathType)},
			Description: r.StructName,
			OperationID: r.Path[index:],
			Responses:   make(openapi3.Responses, 0),
		}

		schema, _ := openapi3gen.NewSchemaRefForValue(r.GetInstance(), nil, openapi3gen.UseAllExportedFields())
		doc.Components.Schemas = make(openapi3.Schemas, 0)
		doc.Components.Schemas[r.StructName] = schema
		info := r.New()
		data, _ := safedo(info, &router.InitRequest{})
		if data != nil {
			res, _ := openapi3gen.NewSchemaRefForValue(data, nil, openapi3gen.UseAllExportedFields())
			oper.AddResponse(200, &openapi3.Response{Content: openapi3.NewContentWithJSONSchema(res.Value)})
		}
		doc.AddOperation(r.Path, r.Method, oper)
	}
}
func safedo(cs types.IRouter, req types.IRequest) (interface{}, error) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("捕获异常:", err)
		}
	}()
	return cs.Do(req)
}
