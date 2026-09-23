package adminui_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// catalogSuite 在通用进程测试能力上增加资料目录专属的 DTO 和路由辅助方法。
type catalogSuite struct {
	*integration.Suite
}

// CategoryDTO 是集成测试关注的分类字段。
type CategoryDTO struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Kind    int    `json:"kind"`
	Enabled bool   `json:"enabled"`
}

// CatalogItemDTO 是集成测试关注的资料条目字段。
type CatalogItemDTO struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	CategoryID uint   `json:"categoryID"`
	Kind       int    `json:"kind"`
	Price      string `json:"price"`
	Stock      int    `json:"stock"`
	Enabled    bool   `json:"enabled"`
}

type tableRows[T any] struct {
	Rows  []T   `json:"rows"`
	Total int64 `json:"total"`
}

var suite *catalogSuite

func TestMain(m *testing.M) {
	created, err := startCatalogSuite()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	suite = created
	code := m.Run()
	if code != 0 {
		suite.PrintLog()
	}
	suite.Stop()
	os.Exit(code)
}

func startCatalogSuite() (*catalogSuite, error) {
	base, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage:     "./examples/08-admin-manage-ui/main",
		BinaryName:       "admin-manage-ui",
		TempPrefix:       "core-admin-manage-ui-",
		ServiceCount:     2,
		ServiceIndex:     1,
		GRPCServiceCount: 2,
		Arguments:        []string{"-view", "0"},
	})
	if err != nil {
		return nil, err
	}
	created := &catalogSuite{Suite: base}
	if err := created.waitReady(); err != nil {
		created.Stop()
		return nil, err
	}
	for _, name := range []string{"server.json", "catalog.json"} {
		if _, err := os.Stat(filepath.Join(created.RootDir, "etc", name)); err != nil {
			created.Stop()
			return nil, fmt.Errorf("框架未自动生成配置 %s: %w", name, err)
		}
	}
	return created, nil
}

func (s *catalogSuite) waitReady() error {
	deadline := time.Now().Add(15 * time.Second)
	lastStatus := 0
	lastEnvelope := integration.ResponseEnvelope{}
	for time.Now().Before(deadline) {
		response, err := http.Get(s.BaseURL + "/api/catalog/getcategories")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var envelope integration.ResponseEnvelope
		_ = json.NewDecoder(response.Body).Decode(&envelope)
		_ = response.Body.Close()
		lastStatus = response.StatusCode
		lastEnvelope = envelope
		if response.StatusCode == http.StatusOK && envelope.Success {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Join(s.RootDir, "service.log"))
	return fmt.Errorf(
		"等待资料目录服务启动超时: last_status=%d last_error=%q last_code=%d\n%s",
		lastStatus,
		lastEnvelope.ErrorMessage,
		lastEnvelope.ErrorCode,
		data,
	)
}

func (s *catalogSuite) searchCategories(t *testing.T, token string) []CategoryDTO {
	t.Helper()
	response := s.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/search", token, map[string]int{"page": 1, "size": 50})
	require.True(t, response.Success, response.ErrorMessage)
	var table tableRows[CategoryDTO]
	require.NoError(t, json.Unmarshal(response.Data, &table), string(response.Data))
	return table.Rows
}

func (s *catalogSuite) addCategory(t *testing.T, token, code, name string, kind int) CategoryDTO {
	t.Helper()
	response := s.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/add", token, map[string]interface{}{
		"code": code, "name": name, "kind": kind, "enabled": true,
	})
	require.True(t, response.Success, response.ErrorMessage)
	var item CategoryDTO
	require.NoError(t, json.Unmarshal(response.Data, &item))
	require.NotEmpty(t, item.ID)
	return item
}

func (s *catalogSuite) addCatalogItem(t *testing.T, token string, category CategoryDTO, code, name string) CatalogItemDTO {
	t.Helper()
	categoryID, err := strconv.ParseUint(category.ID, 10, 64)
	require.NoError(t, err)
	response := s.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/add", token, map[string]interface{}{
		"code": code, "name": name, "categoryID": categoryID, "kind": category.Kind,
		"price": "19.90", "stock": 3, "enabled": true, "secret": "s3cret", "note": "集成测试",
		"lines": []map[string]interface{}{
			{"name": "默认明细", "quantity": 1, "amount": "19.90", "lineNo": 1, "modelState": 1},
		},
	})
	require.True(t, response.Success, response.ErrorMessage)
	var item CatalogItemDTO
	require.NoError(t, json.Unmarshal(response.Data, &item))
	require.NotEmpty(t, item.ID)
	return item
}

func fieldByName(fields []map[string]interface{}, name string) map[string]interface{} {
	for _, field := range fields {
		if field["field"] == name || field["porpfield"] == name {
			return field
		}
	}
	return nil
}

func commandByName(commands []map[string]interface{}, name string) map[string]interface{} {
	for _, command := range commands {
		if command["command"] == name {
			return command
		}
	}
	return nil
}
