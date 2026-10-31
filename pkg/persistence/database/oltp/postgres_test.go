// 本文件验证 PostgreSQL 配置、连接身份和错误公开契约，不依赖外部数据库。
package oltp

import (
	"errors"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestPostgresConfigIdentity 验证凭证不会明文进入连接键且不同身份不会共池。
func TestPostgresConfigIdentity(t *testing.T) {
	c := types.StorageConfig{Driver: "postgres", Host: "localhost", Username: "u", Password: "p@ ss/'", Database: "one"}
	p := NewPostgres(c)
	other := c
	other.Password = "other"
	require.NotEqual(t, p.GetSyncPoolKey(nil), NewPostgres(other).GetSyncPoolKey(nil))
	require.NotContains(t, p.GetSyncPoolKey(nil), c.Password)
	require.Contains(t, p.dsn("one"), "postgres://")
	require.IsType(t, &Postgres{}, p.Clone())
}

// TestPostgresUniqueErrorContract 验证唯一冲突映射409且保留稳定驱动错误。
func TestPostgresUniqueErrorContract(t *testing.T) {
	source := &pgconn.PgError{Code: "23505"}
	err := normalizePersistenceError(source)
	require.Equal(t, servertypes.ErrorKindConflict, servertypes.ResolvePublicError(err).Kind)
	var target *pgconn.PgError
	require.True(t, errors.As(err, &target))
	require.Same(t, source, target)
}

// TestStorageModelBatch 验证批量指针模型仍能解析结构与实例分库字段。
func TestStorageModelBatch(t *testing.T) {
	type record struct{ Tenant string }
	for _, value := range []interface{}{[]*record{{Tenant: "tenant_a"}}, &[]*record{{Tenant: "tenant_a"}}} {
		model, ok := storageModel(value).(*record)
		require.True(t, ok)
		require.Equal(t, "tenant_a", model.Tenant)
	}
	require.IsType(t, &record{}, storageModel([]*record{}))
}
