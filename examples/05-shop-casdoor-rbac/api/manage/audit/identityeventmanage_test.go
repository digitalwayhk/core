package audit

import (
	"testing"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
)

type identityEventManageTestRequest struct {
	servertypes.IRequest
}

func (*identityEventManageTestRequest) GetUser() (string, string) { return "admin-1", "管理员" }
func (*identityEventManageTestRequest) ServiceName() string       { return "casdoorrbacshop" }
func (*identityEventManageTestRequest) GetPath() string {
	return "/api/manage/casdoorrbacshop/identityeventmanage/search"
}
func (*identityEventManageTestRequest) GetTraceId() string { return "trace-identity-audit" }

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

func TestIdentityEventManageOverridesServiceSearchLimit(t *testing.T) {
	manager := NewIdentityEventManage()
	search := managepkg.NewSearch[models.IdentityEventRecord](manager)
	search.SearchItem = &view.SearchItem{Size: 500}

	_, err, stop := manager.SearchBefore(search, &identityEventManageTestRequest{})

	assert.NoError(t, err)
	assert.False(t, stop)
	assert.Equal(t, IdentityEventMaxPageSize, search.SearchItem.Size)
	assert.Len(t, search.SearchItem.SortList, 1)
}
