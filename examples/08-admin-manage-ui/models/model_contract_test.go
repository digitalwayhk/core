package models

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type categoryPersistence interface {
	Query(id uint, name string) ([]*Category, error)
	FindByID(id uint) (*Category, error)
	CodeOrNameExists(code, name string, excludeID uint) (bool, error)
}

type catalogItemPersistence interface {
	FindByID(id uint) (*CatalogItem, error)
	CodeExists(code string, excludeID uint) (bool, error)
	SaveLines() error
	SaveSpecs() error
	SaveChildren() error
	LoadChildren() error
}

type catalogLinePersistence interface {
	QueryByItemID(itemID uint) ([]*CatalogLine, error)
}

type catalogSpecPersistence interface {
	QueryByItemID(itemID uint) ([]*CatalogSpec, error)
}

var _ categoryPersistence = (*Category)(nil)
var _ catalogItemPersistence = (*CatalogItem)(nil)
var _ catalogLinePersistence = (*CatalogLine)(nil)
var _ catalogSpecPersistence = (*CatalogSpec)(nil)

// TestCategoryHashUsesTrimmedCode 验证分类哈希只由规范化编码决定。
func TestCategoryHashUsesTrimmedCode(t *testing.T) {
	category := NewCategory()
	category.Code = "  Electronics  "
	assert.Equal(t, utils.HashCodes("electronics"), category.GetHash())
}

// TestCatalogItemHashUsesTrimmedCode 验证资料条目哈希只由规范化编码决定。
func TestCatalogItemHashUsesTrimmedCode(t *testing.T) {
	item := NewCatalogItem()
	item.Code = "  SKU-1  "
	assert.Equal(t, utils.HashCodes("sku-1"), item.GetHash())
}

// TestCatalogLineHashUsesItemLineAndName 验证明细哈希由条目、行号和名称共同决定。
func TestCatalogLineHashUsesItemLineAndName(t *testing.T) {
	line := NewCatalogLine()
	line.ItemID = 9
	line.LineNo = 1
	line.Name = " 黑色 "
	assert.Equal(t, utils.HashCodes("9", "1", "黑色"), line.GetHash())
}

// TestCatalogSpecHashUsesItemAndName 验证规格哈希由条目和参数名决定。
func TestCatalogSpecHashUsesItemAndName(t *testing.T) {
	spec := NewCatalogSpec()
	spec.ItemID = 9
	spec.SpecName = " 材质 "
	assert.Equal(t, utils.HashCodes("9", "材质"), spec.GetHash())
}

// TestCatalogItemValidateRejectsMissingCategory 验证条目缺少分类时返回公开校验错误。
func TestCatalogItemValidateRejectsMissingCategory(t *testing.T) {
	item := NewCatalogItem()
	item.Code = "sku-1"
	item.Name = "演示"
	item.Price = decimal.RequireFromString("1.00")
	err := item.AddValid()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "请选择分类")
}

// TestModelDataActionIsSingleton 验证模型层复用同一个 IDataAction。
func TestModelDataActionIsSingleton(t *testing.T) {
	first := getDataAction()
	require.NotNil(t, first)
	assert.Same(t, first, getDataAction())
}

// TestServiceModelsShareDatabaseName 验证基础资料和子行共用同一服务库名。
func TestServiceModelsShareDatabaseName(t *testing.T) {
	assert.Equal(t, databaseName, NewCategory().GetLocalDBName())
	assert.Equal(t, databaseName, NewCatalogItem().GetLocalDBName())
	assert.Equal(t, databaseName, NewCatalogLine().GetLocalDBName())
	assert.Equal(t, databaseName, NewCatalogSpec().GetLocalDBName())
}
