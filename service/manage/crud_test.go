package manage_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check: extendedMockDataAction satisfies IDataAction.
var _ pt.IDataAction = (*extendedMockDataAction)(nil)

// testItem is a minimal model used across CRUD tests.
type testItem struct {
	*entity.Model
	Name string `json:"Name"`
}

func (t *testItem) NewModel() {
	if t.Model == nil {
		t.Model = &entity.Model{}
	}
}

// It uses a pre-initialised ModelList so tests control the DB adapter.
type testManageSvc[T pt.IModel] struct {
	// DoBefore control
	doBeforeResult interface{}
	doBeforeErr    error
	doBeforeStop   bool
	// DoAfter tracking
	doAfterCalled bool
	// Persistent list instance (same reference returned by every GetList call)
	cachedList *entity.ModelList[T]
}

func newTestManageSvc[T pt.IModel](ada pt.IDataAction) *testManageSvc[T] {
	list := entity.NewModelList[T](nil)
	if ada != nil {
		list.SetDBAdapter(ada)
	}
	return &testManageSvc[T]{cachedList: list}
}

// IManageService
func (s *testManageSvc[T]) Routers() []st.IRouter                      { return nil }
func (s *testManageSvc[T]) ParseBefore(interface{}, st.IRequest) error { return nil }
func (s *testManageSvc[T]) ParseAfter(interface{}, st.IRequest) error  { return nil }
func (s *testManageSvc[T]) ValidationBefore(interface{}, st.IRequest) (error, bool) {
	return nil, false
}
func (s *testManageSvc[T]) ValidationAfter(interface{}, st.IRequest) error { return nil }
func (s *testManageSvc[T]) DoBefore(interface{}, st.IRequest) (interface{}, error, bool) {
	return s.doBeforeResult, s.doBeforeErr, s.doBeforeStop
}
func (s *testManageSvc[T]) DoAfter(interface{}, st.IRequest) (interface{}, error) {
	s.doAfterCalled = true
	return nil, nil
}

// IManageView
func (s *testManageSvc[T]) GetView() *view.ViewModel                     { return nil }
func (s *testManageSvc[T]) ViewModel(*view.ViewModel)                    {}
func (s *testManageSvc[T]) ViewFieldModel(interface{}, *view.FieldModel) {}
func (s *testManageSvc[T]) ViewCommandModel(*view.CommandModel)          {}
func (s *testManageSvc[T]) ViewChildModel(*view.ViewChildModel)          {}

// IGetModelList
func (s *testManageSvc[T]) GetList() interface{} { return s.cachedList }

// mockDataAction captures Insert calls and returns nil errors.
type mockDataAction struct {
	inserted []interface{}
}

func (m *mockDataAction) Transaction() error                     { return nil }
func (m *mockDataAction) Load(*pt.SearchItem, interface{}) error { return nil }
func (m *mockDataAction) Insert(data interface{}) error {
	m.inserted = append(m.inserted, data)
	return nil
}
func (m *mockDataAction) Update(interface{}) error                    { return nil }
func (m *mockDataAction) Delete(interface{}) error                    { return nil }
func (m *mockDataAction) Raw(string, interface{}) error               { return nil }
func (m *mockDataAction) Exec(string, interface{}) error              { return nil }
func (m *mockDataAction) GetModelDB(interface{}) (interface{}, error) { return nil, nil }
func (m *mockDataAction) Commit() error                               { return nil }
func (m *mockDataAction) GetRunDB() interface{}                       { return nil }
func (m *mockDataAction) Rollback() error                             { return nil }

// crudRequest is a minimal IRequest for CRUD operation tests.
type crudRequest struct {
	body interface{}
}

func (r *crudRequest) ServiceName() string { return "crud-test-svc" }
func (r *crudRequest) Bind(v interface{}) error {
	if r.body == nil {
		return nil
	}
	data, err := json.Marshal(r.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
func (r *crudRequest) GoZeroBind(interface{}) error                { return nil }
func (r *crudRequest) GetTraceId() string                          { return "" }
func (r *crudRequest) GetUser() (string, string)                   { return "", "" }
func (r *crudRequest) GetClientIP() string                         { return "127.0.0.1" }
func (r *crudRequest) NewID() uint                                 { return 1 }
func (r *crudRequest) Authorized() bool                            { return true }
func (r *crudRequest) GetValue(string) string                      { return "" }
func (r *crudRequest) NewResponse(interface{}, error) st.IResponse { return nil }
func (r *crudRequest) GetPath() string                             { return "" }
func (r *crudRequest) GetClaims(string) interface{}                { return nil }
func (r *crudRequest) GetServerInfo() *st.TargetInfo               { return nil }
func (r *crudRequest) GetTargetServerInfo(string) *st.TargetInfo   { return nil }
func (r *crudRequest) CallService(st.IRouter, ...func(st.IResponse)) (st.IResponse, error) {
	return nil, nil
}
func (r *crudRequest) CallTargetService(st.IRouter, *st.TargetInfo, ...func(st.IResponse)) (st.IResponse, error) {
	return nil, nil
}

// --- View tests ---

// TestView_GeneratesViewModel_WithNameField verifies that View.Do returns a
// ViewModel that contains a FieldModel for the testItem.Name field.
func TestView_GeneratesViewModel_WithNameField(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	v := manage.NewView[testItem](svc)

	req := &crudRequest{}
	result, err := v.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)

	vm, ok := result.(*view.ViewModel)
	require.True(t, ok, "View.Do should return *view.ViewModel")

	var found bool
	for _, f := range vm.Fields {
		if f.Field == "Name" || f.PropField == "Name" {
			found = true
			break
		}
	}
	assert.True(t, found, "ViewModel should contain a FieldModel for the Name field")
}

// --- Add tests ---

// TestAdd_DoBeforeStop_ShortCircuits verifies that when DoBefore returns stop=true,
// Do returns the DoBefore result immediately without touching the model list.
func TestAdd_DoBeforeStop_ShortCircuits(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeResult = "stopped"
	svc.doBeforeStop = true

	a := manage.NewAdd[testItem](svc)
	req := &crudRequest{}

	result, err := a.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "stopped", result)
}

// TestAdd_DoBeforeError_PropagatesError verifies that when DoBefore returns a
// non-nil error (and stop=false), Do propagates that error.
func TestAdd_DoBeforeError_PropagatesError(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeErr = errors.New("hook error")

	a := manage.NewAdd[testItem](svc)
	req := &crudRequest{}

	_, err := a.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook error")
}

// TestAdd_HappyPath_MockInsert verifies that Add.Do with a mock DB adapter calls
// Insert exactly once with the parsed item.
func TestAdd_HappyPath_MockInsert(t *testing.T) {
	da := &mockDataAction{}
	svc := newTestManageSvc[testItem](da)

	a := manage.NewAdd[testItem](svc)
	req := &crudRequest{body: &testItem{Name: "test-name"}}

	err := a.Parse(req)
	require.NoError(t, err)

	result, err := a.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, da.inserted, 1, "Insert should be called exactly once")
	item, ok := da.inserted[0].(*testItem)
	require.True(t, ok)
	assert.Equal(t, "test-name", item.Name)
}

// TestAdd_DoAfter_CalledAfterSuccessfulInsert verifies that DoAfter is invoked
// after a successful Add.
func TestAdd_DoAfter_CalledAfterSuccessfulInsert(t *testing.T) {
	da := &mockDataAction{}
	svc := newTestManageSvc[testItem](da)

	a := manage.NewAdd[testItem](svc)
	req := &crudRequest{body: &testItem{Name: "after-test"}}

	require.NoError(t, a.Parse(req))
	_, err := a.Do(req)
	require.NoError(t, err)

	assert.True(t, svc.doAfterCalled, "DoAfter should be called after a successful insert")
}

// loadFunc is called by mockDataAction.Load to populate results.
// The function signature must cast result to the appropriate slice pointer.
type loadResult struct {
	items interface{} // the slice to copy into the Load result
	err   error
}

// extendedMockDataAction extends mockDataAction with per-operation tracking
// and a loadFunc seam for controlled SearchId / LoadList results.
type extendedMockDataAction struct {
	mockDataAction
	loadFn  func(si *pt.SearchItem, result interface{}) error
	updated []interface{}
	deleted []interface{}
}

func (m *extendedMockDataAction) Update(v interface{}) error {
	m.updated = append(m.updated, v)
	return nil
}
func (m *extendedMockDataAction) Delete(v interface{}) error {
	m.deleted = append(m.deleted, v)
	return nil
}
func (m *extendedMockDataAction) Load(si *pt.SearchItem, result interface{}) error {
	if m.loadFn != nil {
		return m.loadFn(si, result)
	}
	return nil
}

// testSearchManageSvc embeds testManageSvc and additionally implements IManageSearch.
type testSearchManageSvc[T pt.IModel] struct {
	*testManageSvc[T]
	searchBeforeResult interface{}
	searchBeforeErr    error
	searchBeforeStop   bool
	searchAfterCalled  bool
}

func newTestSearchManageSvc[T pt.IModel](ada pt.IDataAction) *testSearchManageSvc[T] {
	return &testSearchManageSvc[T]{
		testManageSvc: newTestManageSvc[T](ada),
	}
}

func (s *testSearchManageSvc[T]) SearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	return s.searchBeforeResult, s.searchBeforeErr, s.searchBeforeStop
}
func (s *testSearchManageSvc[T]) SearchAfter(sender interface{}, result *view.TableData, req st.IRequest) (interface{}, error) {
	s.searchAfterCalled = true
	return result, nil
}
func (s *testSearchManageSvc[T]) ForeignSearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}
func (s *testSearchManageSvc[T]) ForeignSearchAfter(sender interface{}, result *view.ForeigData, req st.IRequest) (interface{}, error) {
	return result, nil
}
func (s *testSearchManageSvc[T]) ChildSearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	return nil, nil, false
}
func (s *testSearchManageSvc[T]) ChildSearchAfter(sender interface{}, result *view.TableData, req st.IRequest) (interface{}, error) {
	return result, nil
}
func (s *testSearchManageSvc[T]) OnSearchData(list interface{}, total int64) *view.TableData {
	return &view.TableData{Total: total}
}

// --- Search tests ---

// TestSearch_SearchBefore_Stop verifies that when SearchBefore returns stop=true,
// Search.Do returns the hook result immediately without calling LoadList.
func TestSearch_SearchBefore_Stop(t *testing.T) {
	svc := newTestSearchManageSvc[testItem](nil)
	svc.searchBeforeResult = "search-stopped"
	svc.searchBeforeStop = true

	s := manage.NewSearch[testItem](svc)
	s.New(svc)

	req := &crudRequest{}
	s.SearchItem = &view.SearchItem{Page: 1, Size: 10}

	result, err := s.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "search-stopped", result)
}

// TestSearch_SearchBefore_Error verifies that when SearchBefore returns a
// non-nil error (stop=false), Search.Do propagates it as nil result + error.
func TestSearch_SearchBefore_Error(t *testing.T) {
	svc := newTestSearchManageSvc[testItem](nil)
	svc.searchBeforeErr = errors.New("search hook error")
	svc.searchBeforeStop = false

	s := manage.NewSearch[testItem](svc)
	s.New(svc)

	req := &crudRequest{}
	s.SearchItem = &view.SearchItem{Page: 1, Size: 10}

	result, err := s.Do(req)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "search hook error")
}

// --- Edit tests ---

// TestEdit_DoBeforeStop_ShortCircuits verifies that when DoBefore returns
// stop=true in Edit.Do, the result is returned immediately.
func TestEdit_DoBefore_Stop(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeResult = "edit-stopped"
	svc.doBeforeStop = true

	e := manage.NewEdit[testItem](svc)
	e.New(svc)

	req := &crudRequest{}
	e.Model = &testItem{}

	result, err := e.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "edit-stopped", result)
}

// TestEdit_DoBefore_Error verifies that when DoBefore returns an error,
// Edit.Do propagates it.
func TestEdit_DoBefore_Error(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeErr = errors.New("edit hook error")

	e := manage.NewEdit[testItem](svc)
	e.New(svc)

	req := &crudRequest{}
	e.Model = &testItem{}

	_, err := e.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edit hook error")
}

// --- Remove tests ---

// TestRemove_DoBefore_Stop verifies DoBefore stop short-circuit in Remove.Do.
func TestRemove_DoBefore_Stop(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeResult = "remove-stopped"
	svc.doBeforeStop = true

	r := manage.NewRemove[testItem](svc)
	r.New(svc)

	req := &crudRequest{}
	r.Model = &testItem{}

	result, err := r.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "remove-stopped", result)
}

// TestRemove_DoBefore_Error verifies DoBefore error propagation in Remove.Do.
func TestRemove_DoBefore_Error(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeErr = errors.New("remove hook error")

	r := manage.NewRemove[testItem](svc)
	r.New(svc)

	req := &crudRequest{}
	r.Model = &testItem{}

	_, err := r.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove hook error")
}

// TestRemove_HappyPath_CallsDelete verifies that Remove.Do calls Delete once
// when no DoBefore hook is set.
func TestRemove_HappyPath_CallsDelete(t *testing.T) {
	da := &extendedMockDataAction{}
	svc := newTestManageSvc[testItem](da)

	r := manage.NewRemove[testItem](nil)
	r.New(svc)

	item := &testItem{Name: "to-remove"}
	item.NewModel()
	r.Model = item

	req := &crudRequest{}
	_, err := r.Do(req)
	require.NoError(t, err)
	require.Len(t, da.deleted, 1, "Delete should be called once for Remove happy path")
}

// --- testBaseItem for Submit (requires *entity.BaseModel embedding) ---

type testBaseItem struct {
	*entity.BaseModel
	Title string `json:"Title"`
}

func (t *testBaseItem) NewModel() {
	if t.BaseModel == nil {
		t.BaseModel = entity.NewBaseModel()
	}
}

func newTestBaseItem(id uint, state int) *testBaseItem {
	bm := entity.NewBaseModel()
	bm.Model.ID = id
	bm.State = state
	bm.Code = fmt.Sprintf("item-%d", id) // UpdateValid requires non-empty Code
	return &testBaseItem{BaseModel: bm}
}

// --- Submit tests ---

// TestSubmit_DoBefore_Stop verifies that when DoBefore returns stop=true in
// Submit.Do (after SearchId succeeds), the result is returned immediately.
func TestSubmit_DoBefore_Stop(t *testing.T) {
	// Provide a load function that returns one item so SearchId succeeds.
	da := &extendedMockDataAction{
		loadFn: func(si *pt.SearchItem, result interface{}) error {
			if slice, ok := result.(*[]*testBaseItem); ok {
				item := &testBaseItem{Title: "loaded"}
				item.NewModel()
				item.BaseModel.Model.ID = 1
				*slice = append(*slice, item)
			}
			return nil
		},
	}
	svc := newTestManageSvc[testBaseItem](da)
	svc.doBeforeResult = "submit-stopped"
	svc.doBeforeStop = true

	sub := manage.NewSubmit[testBaseItem](nil)
	sub.New(svc)

	req := &crudRequest{}
	item := &testBaseItem{Title: "test"}
	item.NewModel()
	item.BaseModel.Model.ID = 1
	sub.Model = item

	result, err := sub.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "submit-stopped", result)
}

// --- Release tests ---

// TestRelease_DoBefore_Stop verifies that when DoBefore returns stop=true,
// Release.Do returns the hook result immediately.
func TestRelease_DoBefore_Stop(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeResult = "release-stopped"
	svc.doBeforeStop = true

	rel := manage.NewRelease[testItem](svc)
	rel.New(svc)

	item := &testItem{Name: "test"}
	item.NewModel()
	item.Model.ID = 1 // non-zero ID
	rel.Model = item

	req := &crudRequest{}
	result, err := rel.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "release-stopped", result)
}

// TestRelease_DoBefore_Error verifies DoBefore error propagation in Release.Do.
func TestRelease_DoBefore_Error(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)
	svc.doBeforeErr = errors.New("release hook error")

	rel := manage.NewRelease[testItem](svc)
	rel.New(svc)

	item := &testItem{Name: "test"}
	item.NewModel()
	item.Model.ID = 1
	rel.Model = item

	req := &crudRequest{}
	_, err := rel.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "release hook error")
}

// TestSearch_HappyPath_LoadsListAndCallsSearchAfter verifies the full happy path:
// LoadList populates searchList via the DA, OnSearchData builds a TableData, and
// SearchAfter is invoked with that TableData.
func TestSearch_HappyPath_LoadsListAndCallsSearchAfter(t *testing.T) {
	da := &extendedMockDataAction{
		loadFn: func(si *pt.SearchItem, result interface{}) error {
			if slice, ok := result.(*[]*testItem); ok {
				item := &testItem{Name: "found-item"}
				item.NewModel()
				item.Model.ID = 10
				*slice = append(*slice, item)
				si.Total = 1
			}
			return nil
		},
	}
	svc := newTestSearchManageSvc[testItem](da)

	s := manage.NewSearch[testItem](svc)
	s.New(svc)

	req := &crudRequest{}
	s.SearchItem = &view.SearchItem{Page: 1, Size: 10}

	result, err := s.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)

	td, ok := result.(*view.TableData)
	require.True(t, ok, "Search.Do should return *view.TableData")
	assert.Equal(t, int64(1), td.Total, "TableData.Total should reflect LoadList result")
	assert.True(t, svc.searchAfterCalled, "SearchAfter should be called after LoadList")
}

// TestEdit_HappyPath_UpdatesOldItemAndSaves verifies the full Edit happy path:
// Validation loads the old item via SearchId, Do merges the new fields into the
// old item, calls list.Update + list.Save (triggering da.Update), and then calls DoAfter.
func TestEdit_HappyPath_UpdatesOldItemAndSaves(t *testing.T) {
	da := &extendedMockDataAction{
		loadFn: func(si *pt.SearchItem, result interface{}) error {
			if slice, ok := result.(*[]*testItem); ok {
				old := &testItem{Name: "old-name"}
				old.NewModel()
				old.Model.ID = 5
				*slice = append(*slice, old)
			}
			return nil
		},
	}
	svc := newTestManageSvc[testItem](da)

	e := manage.NewEdit[testItem](nil)
	e.New(svc)

	item := &testItem{Name: "new-name"}
	item.NewModel()
	item.Model.ID = 5
	e.Model = item

	req := &crudRequest{}
	err := e.Validation(req)
	require.NoError(t, err, "Validation should succeed when SearchId finds the old item")

	_, err = e.Do(req)
	require.NoError(t, err)

	// Save() calls da.Update once per entry in updateList.
	require.Len(t, da.updated, 1, "Update should be called once via Save()")
	updated, ok := da.updated[0].(*testItem)
	require.True(t, ok)
	assert.Equal(t, "new-name", updated.Name, "merged name should reflect the new value")
	assert.True(t, svc.doAfterCalled, "DoAfter should be called after a successful edit")
}

// testSubmitSvc is kept as a type alias for possible future use, but currently
// entity.BaseModel cannot be used as T pt.IModel because GetID and Equals on
// *entity.BaseModel have pointer receivers — only *entity.BaseModel satisfies IModel.
// Until those methods gain value receivers, Submit's state-change path cannot be
// covered via the generic API in unit tests.
type testSubmitSvc = testManageSvc[testItem]

// TestSubmit_HappyPath_DoAfterCalled verifies the observable Submit.Do contract:
// SearchId loads the item, DoBefore and DoAfter are called, and the function
// returns without error.
//
// Note: the State 0→1 transition in Submit.Do is gated on getbaseModel(*T)
// returning a non-nil *entity.BaseModel. Since testItem is not *entity.BaseModel
// the state-change block is safely skipped (getbaseModel now uses , ok form).
func TestSubmit_HappyPath_DoAfterCalled(t *testing.T) {
	da := &extendedMockDataAction{
		loadFn: func(si *pt.SearchItem, result interface{}) error {
			if slice, ok := result.(*[]*testItem); ok {
				item := &testItem{Name: "submit-me"}
				item.NewModel()
				item.Model.ID = 7
				*slice = append(*slice, item)
			}
			return nil
		},
	}
	svc := newTestManageSvc[testItem](da)

	sub := manage.NewSubmit[testItem](nil)
	sub.New(svc)

	item := &testItem{Name: "submit-me"}
	item.NewModel()
	item.Model.ID = 7
	sub.Model = item

	req := &crudRequest{}
	_, err := sub.Do(req)
	require.NoError(t, err)

	assert.True(t, svc.doAfterCalled, "DoAfter should be called after successful Submit.Do")
}

// TestSubmit_HappyPath_StateChangeAndSave verifies that Submit.Do sets State
// from 0 to 1 and calls Update+Save when T embeds *entity.BaseModel.
func TestSubmit_HappyPath_StateChangeAndSave(t *testing.T) {
	existing := newTestBaseItem(10, 0) // State=0

	da := &extendedMockDataAction{
		loadFn: func(si *pt.SearchItem, result interface{}) error {
			if slice, ok := result.(*[]*testBaseItem); ok {
				*slice = append(*slice, existing)
			}
			return nil
		},
	}
	svc := newTestManageSvc[testBaseItem](da)

	sub := manage.NewSubmit[testBaseItem](nil)
	sub.New(svc)
	sub.Model = newTestBaseItem(10, 0) // same ID

	req := &crudRequest{}
	_, err := sub.Do(req)
	require.NoError(t, err)

	assert.Equal(t, 1, existing.State, "Submit.Do should set State from 0 to 1")
	require.Len(t, da.updated, 1, "Update should be called once via Save()")
	updated, ok := da.updated[0].(*testBaseItem)
	require.True(t, ok, "updated item should be *testBaseItem")
	assert.Equal(t, 1, updated.State, "Updated item should have State=1")
	assert.True(t, svc.doAfterCalled, "DoAfter should be called after successful Submit.Do")
}

// for update (adds to updateList) when no DoBefore hook is set.
// Note: Release.Do does not call list.Save, so the DB adapter is not invoked;
// the item lands in UpdateArray() instead.
func TestRelease_HappyPath_CallsUpdate(t *testing.T) {
	da := &extendedMockDataAction{
		loadFn: func(si *pt.SearchItem, result interface{}) error {
			if slice, ok := result.(*[]*testItem); ok {
				i2 := &testItem{Name: "release-me"}
				i2.NewModel()
				i2.Model.ID = 42
				*slice = append(*slice, i2)
			}
			return nil
		},
	}
	svc := newTestManageSvc[testItem](da)

	rel := manage.NewRelease[testItem](nil)
	rel.New(svc)

	item := &testItem{Name: "release-me"}
	item.NewModel()
	item.Model.ID = 42
	rel.Model = item

	req := &crudRequest{}
	result, err := rel.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, svc.cachedList.UpdateArray(), 1, "item should be enqueued for update")
}
