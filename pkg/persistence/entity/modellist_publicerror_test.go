package entity

import (
	"testing"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type duplicateModelListItem struct {
	*Model
}

func (item *duplicateModelListItem) NewModel() {
	if item.Model == nil {
		item.Model = NewModel()
	}
}

type duplicateModelListAction struct{}

func (*duplicateModelListAction) Transaction() error { return nil }
func (*duplicateModelListAction) Load(_ *persistencetypes.SearchItem, result interface{}) error {
	items := result.(*[]*duplicateModelListItem)
	existing := &duplicateModelListItem{Model: NewModel()}
	existing.ID = 851993083
	*items = append(*items, existing)
	return nil
}
func (*duplicateModelListAction) Insert(interface{}) error                    { return nil }
func (*duplicateModelListAction) Update(interface{}) error                    { return nil }
func (*duplicateModelListAction) Delete(interface{}) error                    { return nil }
func (*duplicateModelListAction) Raw(string, interface{}) error               { return nil }
func (*duplicateModelListAction) Exec(string, interface{}) error              { return nil }
func (*duplicateModelListAction) GetModelDB(interface{}) (interface{}, error) { return nil, nil }
func (*duplicateModelListAction) Commit() error                               { return nil }
func (*duplicateModelListAction) GetRunDB() interface{}                       { return nil }
func (*duplicateModelListAction) Rollback() error                             { return nil }

// TestModelListAddDuplicateIDOrHashReturnsSafeConflict 验证 ID/hash 写前重复检查均不公开内部数据。
func TestModelListAddDuplicateIDOrHashReturnsSafeConflict(t *testing.T) {
	tests := []struct {
		name string
		item *duplicateModelListItem
	}{
		{
			name: "id",
			item: &duplicateModelListItem{Model: &Model{ID: 851993083}},
		},
		{
			name: "hash",
			item: &duplicateModelListItem{Model: &Model{Hashcode: "private-record-hash"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := NewModelList[duplicateModelListItem](&duplicateModelListAction{})

			err := list.Add(tt.item)
			require.Error(t, err)
			contract := servertypes.ResolvePublicError(err)
			require.Equal(t, servertypes.ErrorKindConflict, contract.Kind)
			require.Equal(t, servertypes.PublicCodeConflict, contract.Code)
			require.Equal(t, 409, contract.HTTPStatus)
			require.Equal(t, "record already exists", contract.Message)
			require.NotContains(t, contract.Message, "851993083")
			require.NotContains(t, contract.Message, "private-record-hash")
			require.NotContains(t, contract.Message, "SQL")
		})
	}
}
