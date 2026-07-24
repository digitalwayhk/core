package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetiredTopLevelConfigFieldsStayAbsent(t *testing.T) {
	serverConfig := reflect.TypeOf(ServerConfig{})
	for _, name := range []string{
		"RunIp",
		"ParentServerIP",
		"AttachServices",
		"Debug",
		"CustomerDataList",
	} {
		_, exists := serverConfig.FieldByName(name)
		require.False(t, exists, "%s 不得重新进入持久化配置", name)
	}
}

func TestMigrateConfigRemovesRetiredTopLevelAndLogtoFields(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
		"RunIp":"127.0.0.1",
		"ParentServerIP":"127.0.0.2",
		"AttachServices":{"orders":{"Address":"127.0.0.1"}},
		"Debug":true,
		"CustomerDataList":[{"Name":"legacy"}],
		"FutureField":{"enabled":true},
		"Auth":{"Logto":{"Enable":true},"AccessSecret":"keep"},
		"ManageAuth":{"Logto":{"Enable":true}},
		"ServerManageAuth":{"Logto":{"Enable":true}}
	}`)
	require.NoError(t, os.WriteFile(file, data, 0o600))
	require.NoError(t, migrateConfig(file))

	migrated, err := os.ReadFile(file)
	require.NoError(t, err)
	var values map[string]interface{}
	require.NoError(t, json.Unmarshal(migrated, &values))
	for _, name := range []string{
		"RunIp",
		"ParentServerIP",
		"AttachServices",
		"Debug",
		"CustomerDataList",
	} {
		require.NotContains(t, values, name)
	}
	require.Equal(t, map[string]interface{}{"enabled": true}, values["FutureField"])
	for _, name := range []string{"Auth", "ManageAuth", "ServerManageAuth"} {
		auth := values[name].(map[string]interface{})
		require.NotContains(t, auth, "Logto")
	}
	require.Equal(t, "keep", values["Auth"].(map[string]interface{})["AccessSecret"])
}
