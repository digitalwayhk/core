package manage

import (
	"testing"

	"github.com/digitalwayhk/core/service/manage/view"
)

func TestAdminUserRoleManageSelectsAdministratorByStableCode(t *testing.T) {
	manager := NewAdminUserRoleManage()
	field := &view.FieldModel{Field: "userCode", PropField: "UserCode", Type: "string"}

	manager.ViewFieldModel(nil, field)

	assertForeignCodeSelector(t, field, "AdminUserModel", "userCode", "username")
}

func TestAdminUserRoleManageSelectsSystemRoleByStableCode(t *testing.T) {
	manager := NewAdminUserRoleManage()
	field := &view.FieldModel{Field: "roleCode", PropField: "RoleCode", Type: "string"}

	manager.ViewFieldModel(nil, field)

	assertForeignCodeSelector(t, field, "ManageRoleModel", "roleCode", "name")
}

func assertForeignCodeSelector(t *testing.T, field *view.FieldModel, objectType, relationField, displayField string) {
	t.Helper()
	if field.Foreign == nil {
		t.Fatal("expected a foreign-model selector")
	}
	if field.Foreign.OneObjectTypeName != objectType {
		t.Fatalf("OneObjectTypeName = %q, want %q", field.Foreign.OneObjectTypeName, objectType)
	}
	if field.Foreign.OneObjectFieldKey != "code" {
		t.Fatalf("OneObjectFieldKey = %q, want stable code", field.Foreign.OneObjectFieldKey)
	}
	if field.Foreign.OneDisplayName != displayField {
		t.Fatalf("OneDisplayName = %q, want %q", field.Foreign.OneDisplayName, displayField)
	}
	if field.Foreign.ManyObjectFieldKey != relationField {
		t.Fatalf("ManyObjectFieldKey = %q, want %q", field.Foreign.ManyObjectFieldKey, relationField)
	}
}
