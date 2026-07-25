package oltp

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
