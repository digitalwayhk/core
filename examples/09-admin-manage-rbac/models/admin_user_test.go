package models

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminUserModelReservesBootstrapAdministratorWithNullableUniqueSlot(t *testing.T) {
	field, ok := reflect.TypeOf(AdminUserModel{}).FieldByName("BootstrapSlot")
	require.True(t, ok, "多实例首用户竞争必须由数据库唯一约束仲裁")
	require.Equal(t, "-", field.Tag.Get("json"))
	require.Contains(t, field.Tag.Get("gorm"), "uniqueIndex")
	require.Equal(t, reflect.Pointer, field.Type.Kind(), "非首用户必须能保存 NULL 并共享该唯一索引")
}
