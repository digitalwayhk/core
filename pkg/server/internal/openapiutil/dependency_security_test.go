package openapiutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/stretchr/testify/require"
)

// 依赖安全回归只在测试中调用 filter，不把它接入 Core 的生产认证链。
func securityDocument(t *testing.T, version, paths string) *openapi3.T {
	t.Helper()
	data := fmt.Sprintf("openapi: %s\ninfo: {title: security-regression, version: '1'}\npaths:\n%s", version, paths)
	doc, err := openapi3.NewLoader().LoadFromData([]byte(data))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(t.Context()))
	return doc
}

func securityRequest(t *testing.T, doc *openapi3.T, method, path, media, body string) *openapi3filter.RequestValidationInput {
	t.Helper()
	router, err := legacy.NewRouter(doc)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if media != "" {
		req.Header.Set("Content-Type", media)
	}
	route, params, err := router.FindRoute(req)
	require.NoError(t, err)
	return &openapi3filter.RequestValidationInput{Request: req, Route: route, PathParams: params}
}

func TestOpenAPIDependencyResponseHeaderContent(t *testing.T) {
	// GHSA-74vm-87hj-r66f：合法 content header 没有 Schema。
	doc := securityDocument(t, "3.0.3", `  /x:
    get:
      responses:
        '200':
          description: ok
          headers:
            X-Thing:
              content:
                application/json: {}
`)
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: securityRequest(t, doc, "GET", "/x", "", ""),
		Status:                 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{}`)),
	}
	require.NotPanics(t, func() { require.NoError(t, openapi3filter.ValidateResponse(t.Context(), in)) })
}

func TestOpenAPIDependencyNonFiniteYAML(t *testing.T) {
	// GHSA-rj39-cv6w-8wv7：必须走 OAS 3.1 的 JSON Schema 数值约束。
	doc := securityDocument(t, "3.1.0", `  /n:
    post:
      requestBody:
        required: true
        content:
          application/yaml:
            schema: {type: number, minimum: 0}
      responses: {'200': {description: ok}}
`)
	for _, body := range []string{".nan", ".inf", "-.inf"} {
		t.Run(body, func(t *testing.T) {
			in := securityRequest(t, doc, "POST", "/n", "application/yaml", body)
			require.NotPanics(t, func() { require.Error(t, openapi3filter.ValidateRequest(t.Context(), in)) })
		})
	}
	in := securityRequest(t, doc, "POST", "/n", "application/yaml", "1")
	require.NoError(t, openapi3filter.ValidateRequest(t.Context(), in))
}

func TestOpenAPIDependencyMissingArrayItems(t *testing.T) {
	// GHSA-vrcc-3ch7-hppw：覆盖 query、deepObject 和组合表单三条解码路径。
	tests := []struct{ name, paths, method, url, media, body string }{
		{"query", `  /x:
    get:
      parameters:
        - {name: ids, in: query, schema: {type: array}}
      responses: {'200': {description: ok}}
`, "GET", "/x?ids=1&ids=2", "", ""},
		{"deepObject", `  /x:
    get:
      parameters:
        - name: filter
          in: query
          style: deepObject
          explode: true
          schema:
            type: object
            properties: {tags: {type: array}}
      responses: {'200': {description: ok}}
`, "GET", "/x?filter[tags][0]=a", "", ""},
		{"formAllOf", `  /x:
    post:
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              allOf:
                - type: object
                  properties: {tags: {type: array}}
      responses: {'200': {description: ok}}
`, "POST", "/x", "application/x-www-form-urlencoded", "tags=a&tags=b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := securityDocument(t, "3.1.0", tt.paths)
			in := securityRequest(t, doc, tt.method, tt.url, tt.media, tt.body)
			require.NotPanics(t, func() {
				err := openapi3filter.ValidateRequest(t.Context(), in)
				// OAS 3.1 的无 items 表单可作为无约束数据接受；query 解码必须返回错误。
				if tt.name != "formAllOf" {
					require.Error(t, err)
				}
			})
		})
	}
}

func TestOpenAPIDependencyEncodedPathAuthorization(t *testing.T) {
	// GHSA-23vg-4cjm-66q5：验证与下游 escaped path 分派不能选中不同操作。
	data := `openapi: 3.0.3
info: {title: authorization-regression, version: '1'}
paths:
  /public/status:
    get:
      responses: {'204': {description: public}}
  /{id}:
    get:
      security: [{key: []}]
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses: {'202': {description: protected}}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-API-Key}
`
	file := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(file, []byte(data), 0600))
	authCalls, protectedCalls := 0, 0
	validator := &openapi3filter.ValidationHandler{File: file,
		AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error {
			authCalls++
			return errors.New("denied")
		},
		ErrorEncoder: func(_ context.Context, _ error, w http.ResponseWriter) { w.WriteHeader(401) },
	}
	require.NoError(t, validator.Load())
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/public/status" {
			w.WriteHeader(204)
			return
		}
		protectedCalls++
		w.WriteHeader(202)
	})
	for _, tt := range []struct {
		path   string
		status int
	}{
		{"/public/status", 204}, {"/ordinary", 401}, {"/public%2Fstatus", 401}, {"/public%2fstatus", 401},
	} {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			validator.Middleware(downstream).ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			require.Equal(t, tt.status, w.Code)
		})
	}
	require.Equal(t, 1, authCalls)
	require.Zero(t, protectedCalls)
}

func TestOpenAPIDependencyCircularSchema(t *testing.T) {
	// GHSA-g8cf-rcjf-c8cf：旧版 fatal stack overflow 不能 recover，隔离到子进程。
	for _, mode := range []string{"visit", "decode"} {
		t.Run(mode, func(t *testing.T) {
			if os.Getenv("CORE_OPENAPI_CYCLE_PROBE") == mode {
				debug.SetMaxStack(1 << 20)
				schema := openapi3.NewObjectSchema()
				schema.AllOf = openapi3.SchemaRefs{{Value: schema}}
				require.NoError(t, schema.Validate(t.Context()))
				if mode == "visit" {
					require.NoError(t, schema.VisitJSON(map[string]any{}))
				} else {
					doc := securityDocument(t, "3.0.3", "  /x:\n    get:\n      responses: {'200': {description: ok}}\n")
					doc.Paths.Value("/x").Get.Parameters = openapi3.Parameters{{Value: &openapi3.Parameter{
						Name: "data", In: "query", Schema: &openapi3.SchemaRef{Value: schema},
					}}}
					in := securityRequest(t, doc, "GET", "/x?data=x", "", "")
					require.ErrorContains(t, openapi3filter.ValidateRequest(t.Context(), in), "circular")
				}
				return
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenAPIDependencyCircularSchema$/^"+mode+"$", "-test.v")
			cmd.Env = append(os.Environ(), "CORE_OPENAPI_CYCLE_PROBE="+mode)
			out, err := cmd.CombinedOutput()
			require.NoError(t, ctx.Err(), "子进程超时：%s", out)
			require.NoError(t, err, "子进程必须存活：%s", out)
		})
	}
}
