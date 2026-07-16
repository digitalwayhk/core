package manage

import (
	"testing"

	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
)

func TestIdentityEventManageIsReadOnlyAndFormatsAuditFields(t *testing.T) {
	manager := NewIdentityEventManage()

	assert.Len(t, manager.Routers(), 2)
	assert.Same(t, manager, manager.View.GetInstance())
	assert.Same(t, manager, manager.Search.GetInstance())

	eventID := &view.FieldModel{Field: "eventID", PropField: "EventID", IsEdit: true}
	manager.ViewFieldModel(nil, eventID)
	assert.Equal(t, "事件 ID", eventID.Title)
	assert.False(t, eventID.IsEdit)

	blocked := &view.FieldModel{Field: "blocked", PropField: "Blocked", IsEdit: true}
	manager.ViewFieldModel(nil, blocked)
	assert.Equal(t, "禁止访问", blocked.Title)
	assert.False(t, blocked.IsEdit)
	assert.True(t, blocked.IsSearch)
}
