package run

import (
	"context"
	"net/http"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestGetOpenAPIWithoutServicesReturnsEmptyDocument(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://compat.example/api/openapi", nil)
	require.NoError(t, err)

	doc, ok := GetOpenApi(req).(*openapi3.T)
	require.True(t, ok)
	require.NotNil(t, doc.Paths)
	require.Zero(t, doc.Paths.Len())
	require.Empty(t, doc.Servers)
	require.Equal(t, "Bearer token authentication", doc.Components.SecuritySchemes["Bearer"].Value.Description)
	require.NoError(t, doc.Validate(context.Background()))
}
