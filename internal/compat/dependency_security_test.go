// 本文件锁定已修复安全公告的最低依赖版本，防止后续依赖整理重新引入漏洞版本。
package compat

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSecurityPatchedDependencyVersions 锁定 CVE-2026-81870 与 CVE-2026-84445 的最低修复版本。
func TestSecurityPatchedDependencyVersions(t *testing.T) {
	versions := goModVersions(t)
	required := map[string]string{
		"go.opentelemetry.io/otel/exporters/otlp/otlptrace":               "v1.45.0",
		"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc": "v1.45.0",
		"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp": "v1.45.0",
		"go.opentelemetry.io/otel/exporters/zipkin":                       "v1.45.0",
		"go.opentelemetry.io/otel/sdk":                                    "v1.45.0",
		"google.golang.org/grpc":                                          "v1.83.2",
	}
	for module, minimum := range required {
		t.Run(module, func(t *testing.T) {
			actual, ok := versions[module]
			require.True(t, ok, "%s must remain explicitly pinned in go.mod", module)
			requireVersionAtLeast(t, actual, minimum)
		})
	}
}

// TestParseGoModVersionsUsesReplacementVersion 验证 replace 不能绕过最低安全版本门禁。
func TestParseGoModVersionsUsesReplacementVersion(t *testing.T) {
	versions := parseGoModVersions(t, strings.NewReader(`module example.test/security

require google.golang.org/grpc v1.83.2
replace google.golang.org/grpc => google.golang.org/grpc v1.83.1
`))
	require.Equal(t, "v1.83.1", versions["google.golang.org/grpc"])
}

func goModVersions(t *testing.T) map[string]string {
	t.Helper()
	file, err := os.Open(filepath.Join(repositoryRoot(t), "go.mod"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	return parseGoModVersions(t, file)
}

func parseGoModVersions(t *testing.T, reader io.Reader) map[string]string {
	t.Helper()
	type replacement struct {
		module     string
		oldVersion string
		newModule  string
		newVersion string
	}

	versions := make(map[string]string)
	var replacements []replacement
	inReplaceBlock := false
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "replace" {
			if len(fields) == 2 && fields[1] == "(" {
				inReplaceBlock = true
				continue
			}
			fields = fields[1:]
		} else if inReplaceBlock {
			if fields[0] == ")" {
				inReplaceBlock = false
				continue
			}
		} else {
			if fields[0] == "require" && len(fields) >= 3 {
				fields = fields[1:]
			}
			if len(fields) >= 2 && strings.Contains(fields[0], ".") && strings.HasPrefix(fields[1], "v") {
				versions[fields[0]] = fields[1]
			}
			continue
		}

		arrow := -1
		for index, field := range fields {
			if field == "=>" {
				arrow = index
				break
			}
		}
		if arrow < 1 || arrow+1 >= len(fields) {
			continue
		}
		item := replacement{module: fields[0], newModule: fields[arrow+1]}
		if arrow == 2 {
			item.oldVersion = fields[1]
		}
		if arrow+2 < len(fields) && strings.HasPrefix(fields[arrow+2], "v") {
			item.newVersion = fields[arrow+2]
		}
		replacements = append(replacements, item)
	}
	require.NoError(t, scanner.Err())
	for _, item := range replacements {
		actual, required := versions[item.module]
		if !required || item.oldVersion != "" && item.oldVersion != actual {
			continue
		}
		if item.newModule != item.module || item.newVersion == "" {
			versions[item.module] = "unverifiable replacement"
			continue
		}
		versions[item.module] = item.newVersion
	}
	return versions
}

func requireVersionAtLeast(t *testing.T, actual, minimum string) {
	t.Helper()
	require.True(t, strings.HasPrefix(actual, "v"),
		"dependency replacement version cannot be verified: %s", actual)
	require.GreaterOrEqual(t, parseReleaseVersion(t, actual), parseReleaseVersion(t, minimum),
		"dependency version %s is below security minimum %s", actual, minimum)
}

func parseReleaseVersion(t *testing.T, version string) int {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	require.Len(t, parts, 3, "security contract expects a stable semantic version: %s", version)
	value := 0
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		require.NoError(t, err, "security contract expects a stable semantic version: %s", version)
		value = value*1_000_000 + n
	}
	return value
}
