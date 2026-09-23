package adminui_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManageAPIs(t *testing.T) {
	t.Run("CategoryManageView", testCategoryManageView)
	t.Run("CategoryManageSearchSeeds", testCategoryManageSearchSeeds)
	t.Run("CategoryManageAddEditRemove", testCategoryManageAddEditRemove)
	t.Run("CategoryManageSubmit", testCategoryManageSubmit)
	t.Run("CatalogItemManageView", testCatalogItemManageView)
	t.Run("CatalogItemManageSearchAndForeignAndChild", testCatalogItemManageSearchAndForeignAndChild)
	t.Run("CatalogItemManageAdd", testCatalogItemManageAdd)
	t.Run("CatalogItemManageClone", testCatalogItemManageClone)
}

func testCategoryManageView(t *testing.T) {
	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/view", suite.TokenFor(t, "category-view-admin", 1), nil)
	require.True(t, response.Success, response.ErrorMessage)
	var model struct {
		Title    string                   `json:"title"`
		AutoLoad bool                     `json:"autoload"`
		Fields   []map[string]interface{} `json:"fields"`
		Commands []map[string]interface{} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &model), string(response.Data))
	assert.Equal(t, "分类管理", model.Title)
	assert.True(t, model.AutoLoad)
	kind := fieldByName(model.Fields, "kind")
	require.NotNil(t, kind)
	comvtp, _ := kind["comvtp"].(map[string]interface{})
	require.NotNil(t, comvtp)
	assert.Equal(t, true, comvtp["isvtp"])
	assert.Equal(t, true, kind["showInComvtp"])
	for _, name := range []string{"add", "edit", "remove", "submit", "release"} {
		require.NotNil(t, commandByName(model.Commands, name), name)
	}
}

func testCategoryManageSearchSeeds(t *testing.T) {
	token := suite.TokenFor(t, "category-seed-admin", 1)
	rows := suite.searchCategories(t, token)
	require.GreaterOrEqual(t, len(rows), 3)
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	assert.Contains(t, names, "电子")
	assert.Contains(t, names, "图书")
	assert.Contains(t, names, "配件")
}

func testCategoryManageAddEditRemove(t *testing.T) {
	token := suite.TokenFor(t, "category-crud-admin", 1)
	code := fmt.Sprintf("tmp-%d", time.Now().UnixNano())
	created := suite.addCategory(t, token, code, "临时分类", 0)
	assert.Equal(t, "临时分类", created.Name)

	edited := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/edit", token, map[string]interface{}{
		"id": created.ID, "code": code, "name": "临时分类-已改", "kind": 1, "enabled": true,
	})
	require.True(t, edited.Success, edited.ErrorMessage)

	removed := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/remove", token, map[string]interface{}{
		"id": created.ID,
	})
	require.True(t, removed.Success, removed.ErrorMessage)
}

func testCategoryManageSubmit(t *testing.T) {
	token := suite.TokenFor(t, "category-submit-admin", 1)
	code := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	created := suite.addCategory(t, token, code, "待提交分类", 2)
	submitted := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/submit", token, map[string]interface{}{
		"id": created.ID,
	})
	require.True(t, submitted.Success, submitted.ErrorMessage)
	blocked := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/categorymanage/remove", token, map[string]interface{}{
		"id": created.ID,
	})
	assert.False(t, blocked.Success)
}

func testCatalogItemManageView(t *testing.T) {
	response := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/view", suite.TokenFor(t, "item-view-admin", 1), nil)
	require.True(t, response.Success, response.ErrorMessage)
	var model struct {
		Fields      []map[string]interface{} `json:"fields"`
		Commands    []map[string]interface{} `json:"commands"`
		ChildModels []map[string]interface{} `json:"childmodels"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &model), string(response.Data))
	category := fieldByName(model.Fields, "categoryID")
	require.NotNil(t, category)
	require.NotNil(t, category["foreign"])
	secret := fieldByName(model.Fields, "secret")
	require.NotNil(t, secret)
	assert.Equal(t, true, secret["ispassword"])
	note := fieldByName(model.Fields, "note")
	require.NotNil(t, note)
	assert.Equal(t, true, note["isremark"])
	importdata := commandByName(model.Commands, "importdata")
	require.NotNil(t, importdata)
	assert.Equal(t, true, importdata["issplit"])
	assert.Equal(t, "add", importdata["splitname"])
	exportdata := commandByName(model.Commands, "exportdata")
	require.NotNil(t, exportdata)
	assert.Equal(t, true, exportdata["issplit"])
	assert.Equal(t, "add", exportdata["splitname"])
	clone := commandByName(model.Commands, "cloneitem")
	require.NotNil(t, clone)
	assert.Equal(t, true, clone["editshow"])
	assert.Equal(t, true, clone["issplit"])
	assert.Equal(t, "add", clone["splitname"])
	require.Len(t, model.ChildModels, 2)
	assert.Equal(t, "Lines", model.ChildModels[0]["name"])
	assert.Equal(t, "明细行", model.ChildModels[0]["title"])
	assert.Equal(t, "Specs", model.ChildModels[1]["name"])
	assert.Equal(t, "规格参数", model.ChildModels[1]["title"])
}

func testCatalogItemManageSearchAndForeignAndChild(t *testing.T) {
	token := suite.TokenFor(t, "item-search-admin", 1)
	_ = suite.searchCategories(t, token)
	search := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/search", token, map[string]int{"page": 1, "size": 50})
	require.True(t, search.Success, search.ErrorMessage)
	var table tableRows[CatalogItemDTO]
	require.NoError(t, json.Unmarshal(search.Data, &table), string(search.Data))
	require.NotEmpty(t, table.Rows)

	viewResponse := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/view", token, nil)
	require.True(t, viewResponse.Success, viewResponse.ErrorMessage)
	var model struct {
		Fields      []map[string]interface{} `json:"fields"`
		ChildModels []map[string]interface{} `json:"childmodels"`
	}
	require.NoError(t, json.Unmarshal(viewResponse.Data, &model))
	categoryField := fieldByName(model.Fields, "categoryID")
	require.NotNil(t, categoryField)
	foreign := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/search", token, map[string]interface{}{
		"page": 1, "size": 20, "field": categoryField, "foreign": categoryField["foreign"],
	})
	require.True(t, foreign.Success, foreign.ErrorMessage)
	var foreignTable struct {
		Rows  []CategoryDTO `json:"rows"`
		Total int64         `json:"total"`
	}
	require.NoError(t, json.Unmarshal(foreign.Data, &foreignTable), string(foreign.Data))
	require.NotEmpty(t, foreignTable.Rows)

	require.Len(t, model.ChildModels, 2)
	for _, childModel := range model.ChildModels {
		child := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/search", token, map[string]interface{}{
			"page": 1, "size": 10, "parent": table.Rows[0], "childmodel": childModel,
		})
		require.True(t, child.Success, child.ErrorMessage)
		var childTable tableRows[map[string]interface{}]
		require.NoError(t, json.Unmarshal(child.Data, &childTable), string(child.Data))
		assert.NotEmpty(t, childTable.Rows, childModel["name"])
	}
}

func testCatalogItemManageAdd(t *testing.T) {
	token := suite.TokenFor(t, "item-add-admin", 1)
	categories := suite.searchCategories(t, token)
	require.NotEmpty(t, categories)
	code := fmt.Sprintf("sku-%d", time.Now().UnixNano())
	item := suite.addCatalogItem(t, token, categories[0], code, "集成测试条目")
	assert.Equal(t, "集成测试条目", item.Name)
}

func testCatalogItemManageClone(t *testing.T) {
	token := suite.TokenFor(t, "item-clone-admin", 1)
	categories := suite.searchCategories(t, token)
	require.NotEmpty(t, categories)
	code := fmt.Sprintf("clone-%d", time.Now().UnixNano())
	item := suite.addCatalogItem(t, token, categories[0], code, "待复制条目")
	cloned := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/cloneitem", token, map[string]interface{}{
		"id": item.ID, "code": code, "name": "待复制条目", "categoryID": item.CategoryID,
		"kind": categories[0].Kind, "price": "19.90", "stock": 3, "enabled": true,
	})
	require.True(t, cloned.Success, cloned.ErrorMessage)
	var copy CatalogItemDTO
	require.NoError(t, json.Unmarshal(cloned.Data, &copy), string(cloned.Data))
	assert.Equal(t, code+"-copy", copy.Code)
	assert.NotEqual(t, item.ID, copy.ID)
}
