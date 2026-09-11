// 本文件验证批量确认必须完整匹配请求，不能把缺失记录当作已确认。
package transaction

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"testing"
)

type incompleteOutboxAction struct {
	persistencetypes.IDataAction
	items   []*OutboxRecord
	updates int
}

func (a *incompleteOutboxAction) Load(_ *persistencetypes.SearchItem, result interface{}) error {
	*result.(*[]*OutboxRecord) = a.items
	return nil
}
func (a *incompleteOutboxAction) Update(interface{}) error { a.updates++; return nil }

// TestBatchConfirmationRejectsIncompleteIDs 验证缺失或零主键必须阻止整批更新。
func TestBatchConfirmationRejectsIncompleteIDs(t *testing.T) {
	for _, ids := range [][]uint{{11, 12}, {11, 0}} {
		item := NewOutbox()
		item.ID = 11
		a := &incompleteOutboxAction{items: []*OutboxRecord{item}}
		require.Error(t, MarkOutboxPublishedByIDs(a, ids))
		require.Zero(t, a.updates)
		require.False(t, item.Published)
	}
}
