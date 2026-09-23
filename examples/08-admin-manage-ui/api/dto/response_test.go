package dto

import (
	"encoding/json"
	"testing"

	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCategoryResponse 验证分类 DTO 只暴露前端需要的字段。
func TestCategoryResponse(t *testing.T) {
	category := models.NewCategory()
	category.ID = 7
	category.Code = "electronics"
	category.Name = "电子"
	category.Kind = models.KindElectronics
	category.Enabled = true

	response := NewCategoryResponse(category)
	require.NotNil(t, response)
	data, err := json.Marshal(response)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"7","code":"electronics","name":"电子","kind":0,"enabled":true}`, string(data))
	assert.NotContains(t, string(data), "hashCode")
	assert.NotContains(t, string(data), "modelState")
}
