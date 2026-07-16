package models

import persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"

// Insert 写入身份事件审计记录。
func (own *IdentityEventRecord) Insert() error {
	return own.InsertWith(getDataAction())
}

// InsertWith 使用指定数据操作器写入身份事件。
func (own *IdentityEventRecord) InsertWith(action persistencetypes.IDataAction) error {
	if err := own.Normalize(); err != nil {
		return err
	}
	own.SetHashcode(own.GetHash())
	return action.Insert(own)
}

// QueryByEventID 按框架事件 ID 查询审计记录。
func (own *IdentityEventRecord) QueryByEventID(eventID string) ([]*IdentityEventRecord, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*IdentityEventRecord
	search := newSearch(own, 2)
	search.AddWhereN("EventID", eventID)
	err := getDataAction().Load(search, &result)
	return result, err
}
