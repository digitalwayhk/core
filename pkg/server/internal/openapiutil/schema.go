package openapiutil

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
)

// SchemaRefForValue 使用 kin-openapi 的类型映射生成参数 Schema。
// 无法推导的类型返回空 Schema，使文档生成保持可用且不伪造类型信息。
func SchemaRefForValue(value any) *openapi3.SchemaRef {
	schema, err := openapi3gen.NewSchemaRefForValue(value, nil, openapi3gen.UseAllExportedFields())
	if err != nil || schema == nil {
		return &openapi3.SchemaRef{Value: openapi3.NewSchema()}
	}
	return schema
}
