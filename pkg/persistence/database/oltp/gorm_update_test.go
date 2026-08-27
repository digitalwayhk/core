package oltp

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type changingHashRecord struct {
	ID       uint `gorm:"primaryKey"`
	Name     string
	Hashcode string `gorm:"column:hashcode;uniqueIndex"`
}

func (record *changingHashRecord) Equals(other interface{}) bool {
	model, ok := other.(types.IModel)
	return ok && model.GetID() == record.ID
}

func (record *changingHashRecord) GetID() uint                 { return record.ID }
func (record *changingHashRecord) SetID(id uint)               { record.ID = id }
func (record *changingHashRecord) GetHash() string             { return record.Hashcode }
func (record *changingHashRecord) SetHashcode(hashcode string) { record.Hashcode = hashcode }

// TestUpdateDataUsesIDWhenHashChanges 验证具有 ID 的模型在哈希改变后仍能定位并更新原记录。
func TestUpdateDataUsesIDWhenHashChanges(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&changingHashRecord{}))
	require.NoError(t, db.Create(&changingHashRecord{ID: 1, Name: "old", Hashcode: "old-hash"}).Error)

	changed := &changingHashRecord{ID: 1, Name: "new", Hashcode: "new-hash"}
	require.NoError(t, updateData(db, changed))

	var stored changingHashRecord
	require.NoError(t, db.First(&stored, 1).Error)
	require.Equal(t, "new", stored.Name)
	require.Equal(t, "new-hash", stored.Hashcode)
}

type scopedLoadRecord struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (*scopedLoadRecord) ScopesHandler() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB { return db }
}

// TestLoadSkipCountExecutesOneSelect 验证业务热路径显式放弃总数时不再执行 COUNT 往返。
func TestLoadSkipCountExecutesOneSelect(t *testing.T) {
	var statements bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.New(
		log.New(&statements, "", 0),
		logger.Config{SlowThreshold: time.Second, LogLevel: logger.Info},
	)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&scopedLoadRecord{}))
	require.NoError(t, db.Create(&scopedLoadRecord{Name: "first"}).Error)
	statements.Reset()

	query := &types.SearchItem{Page: 1, Size: 1, Total: 99, Model: &scopedLoadRecord{}, SkipCount: true}
	query.AddWhereN("Name", "first")
	var rows []*scopedLoadRecord
	require.NoError(t, load(db, query, &rows))
	require.Len(t, rows, 1)
	require.Zero(t, query.Total, "skip-count query must not claim a full result total")

	sql := strings.ToUpper(statements.String())
	require.NotContains(t, sql, "COUNT(")
	require.Equal(t, 1, strings.Count(sql, "SELECT"), statements.String())
}
