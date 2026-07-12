package config

import (
	"bufio"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capabilityMatrixPath = "../../../docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md"

type capabilityMatrixEntry struct {
	Status   string
	Owner    string
	Evidence string
}

func TestCapabilityMatrixExactlyMatchesProjectServerConfigFields(t *testing.T) {
	matrix, err := os.ReadFile(capabilityMatrixPath)
	require.NoError(t, err)

	entries, err := parseCapabilityMatrix(string(matrix))
	require.NoError(t, err)

	codePaths := projectConfigFieldPaths(reflect.TypeOf(ServerConfig{}), "ServerConfig")
	matrixPaths := sortedMatrixPaths(entries)
	missing, stale := sortedPathDifference(codePaths, matrixPaths), sortedPathDifference(matrixPaths, codePaths)
	assert.Empty(t, missing, "能力矩阵缺失字段: %v", missing)
	assert.Empty(t, stale, "能力矩阵包含多余或陈旧字段: %v", stale)
}

func TestParseCapabilityMatrixRejectsInvalidMachineChecklist(t *testing.T) {
	validHeader := "## 机器检查字段清单\n\n| 字段路径 | 状态 | 生命周期 owner | 运行时/拒绝证据 |\n| --- | --- | --- | --- |\n"
	tests := []struct {
		name string
		body string
	}{
		{name: "body mention is not a machine row", body: "正文介绍 `ServerConfig.Debug`\n"},
		{name: "missing status", body: "| `ServerConfig.Debug` | | server | config test |\n"},
		{name: "missing owner", body: "| `ServerConfig.Debug` | supported | | config test |\n"},
		{name: "missing evidence", body: "| `ServerConfig.Debug` | supported | server | |\n"},
		{name: "illegal status", body: "| `ServerConfig.Debug` | pending | server | config test |\n"},
		{name: "placeholder owner", body: "| `ServerConfig.Debug` | supported | TODO | config test |\n"},
		{name: "placeholder evidence", body: "| `ServerConfig.Debug` | supported | server | - |\n"},
		{name: "duplicate path", body: "| `ServerConfig.Debug` | supported | server | config test |\n| `ServerConfig.Debug` | rejected | validator | rejection test |\n"},
		{name: "invalid row", body: "| `ServerConfig.Debug` | supported | server |\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCapabilityMatrix(validHeader + tt.body)
			require.Error(t, err)
		})
	}
}

func TestParseCapabilityMatrixOnlyReadsMachineChecklistSection(t *testing.T) {
	doc := "`ServerConfig.Stale`\n\n## 机器检查字段清单\n\n" +
		"| 字段路径 | 状态 | 生命周期 owner | 运行时/拒绝证据 |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `ServerConfig.Debug` | supported | server | config test |\n\n" +
		"## 其他\n\n| `ServerConfig.AfterSection` | supported | server | config test |\n"

	entries, err := parseCapabilityMatrix(doc)
	require.NoError(t, err)
	assert.Equal(t, map[string]capabilityMatrixEntry{
		"ServerConfig.Debug": {Status: "supported", Owner: "server", Evidence: "config test"},
	}, entries)
}

func TestProjectConfigFieldPathsRecursesIntoSamePackageStructPointers(t *testing.T) {
	type childFixture struct {
		Value string
		Map   map[string]string
		Slice []string
	}
	type rootFixture struct {
		Child *childFixture
	}

	assert.Equal(t, []string{
		"Root.Child",
		"Root.Child.Map",
		"Root.Child.Slice",
		"Root.Child.Value",
	}, projectConfigFieldPaths(reflect.TypeOf(rootFixture{}), "Root"))
}

func TestProjectConfigFieldPathsKeepsRestConfAtEmbeddingPoint(t *testing.T) {
	paths := projectConfigFieldPaths(reflect.TypeOf(ServerConfig{}), "ServerConfig")
	assert.Contains(t, paths, "ServerConfig.RestConf")
	for _, path := range paths {
		assert.False(t, strings.HasPrefix(path, "ServerConfig.RestConf."), "RestConf 不应递归: %s", path)
	}
}

func parseCapabilityMatrix(document string) (map[string]capabilityMatrixEntry, error) {
	const sectionHeading = "## 机器检查字段清单"
	const header = "| 字段路径 | 状态 | 生命周期 owner | 运行时/拒绝证据 |"
	const separator = "| --- | --- | --- | --- |"

	lines := strings.Split(document, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == sectionHeading {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return nil, fmt.Errorf("缺少 %q section", sectionHeading)
	}

	var section []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		if strings.TrimSpace(line) != "" {
			section = append(section, strings.TrimSpace(line))
		}
	}
	if len(section) < 3 || section[0] != header || section[1] != separator {
		return nil, fmt.Errorf("机器检查字段清单必须是严格四列 Markdown 表")
	}

	entries := make(map[string]capabilityMatrixEntry, len(section)-2)
	for lineNumber, line := range section[2:] {
		columns, err := parseCapabilityMatrixRow(line)
		if err != nil {
			return nil, fmt.Errorf("机器检查表第 %d 行: %w", lineNumber+1, err)
		}
		path := strings.TrimSpace(columns[0])
		if len(path) < 3 || path[0] != '`' || path[len(path)-1] != '`' {
			return nil, fmt.Errorf("字段路径必须是单个反引号包裹的路径: %q", path)
		}
		path = path[1 : len(path)-1]
		if _, exists := entries[path]; exists {
			return nil, fmt.Errorf("重复字段路径 %q", path)
		}
		entry := capabilityMatrixEntry{Status: columns[1], Owner: columns[2], Evidence: columns[3]}
		if entry.Status != "supported" && entry.Status != "rejected" && entry.Status != "upstream" {
			return nil, fmt.Errorf("字段 %q 状态非法: %q", path, entry.Status)
		}
		if err := validateCapabilityCell("owner", entry.Owner); err != nil {
			return nil, fmt.Errorf("字段 %q: %w", path, err)
		}
		if err := validateCapabilityCell("evidence", entry.Evidence); err != nil {
			return nil, fmt.Errorf("字段 %q: %w", path, err)
		}
		entries[path] = entry
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("机器检查表不能为空")
	}
	return entries, nil
}

func parseCapabilityMatrixRow(line string) ([]string, error) {
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil, fmt.Errorf("非法 Markdown 表行 %q", line)
	}
	columns := strings.Split(line[1:len(line)-1], "|")
	if len(columns) != 4 {
		return nil, fmt.Errorf("期望 4 列，实际 %d 列", len(columns))
	}
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
		if columns[i] == "" {
			return nil, fmt.Errorf("第 %d 列为空", i+1)
		}
	}
	return columns, nil
}

func validateCapabilityCell(name, value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "-", "todo", "待补":
		return fmt.Errorf("%s 不能为空或占位值", name)
	default:
		return nil
	}
}

func projectConfigFieldPaths(typ reflect.Type, prefix string) []string {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	var paths []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		path := prefix + "." + field.Name
		paths = append(paths, path)

		fieldType := field.Type
		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct && fieldType.PkgPath() == typ.PkgPath() {
			paths = append(paths, projectConfigFieldPaths(fieldType, path)...)
		}
	}
	sort.Strings(paths)
	return paths
}

func sortedMatrixPaths(entries map[string]capabilityMatrixEntry) []string {
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedPathDifference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, path := range right {
		rightSet[path] = struct{}{}
	}
	var difference []string
	for _, path := range left {
		if _, exists := rightSet[path]; !exists {
			difference = append(difference, path)
		}
	}
	sort.Strings(difference)
	return difference
}

func TestCapabilityMatrixHasNoAcceptedButIgnoredStatus(t *testing.T) {
	file, err := os.Open(capabilityMatrixPath)
	require.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		assert.NotContains(t, scanner.Text(), "accepted-but-ignored")
	}
	require.NoError(t, scanner.Err())
}
