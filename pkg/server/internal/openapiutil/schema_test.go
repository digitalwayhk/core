package openapiutil

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestSchemaRefForValueMapsGoScalarsToOpenAPITypes(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "string", value: "", want: openapi3.TypeString},
		{name: "integer", value: int64(0), want: openapi3.TypeInteger},
		{name: "number", value: float64(0), want: openapi3.TypeNumber},
		{name: "boolean", value: false, want: openapi3.TypeBoolean},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := SchemaRefForValue(tt.value)
			require.NotNil(t, schema)
			require.NotNil(t, schema.Value)
			require.True(t, schema.Value.Type.Is(tt.want))
		})
	}
}
