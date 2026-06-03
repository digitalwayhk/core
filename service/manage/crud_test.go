package manage_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// testManageSvc[T] implements IManageService, IManageView, and IGetModelList.
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
func (s *testManageSvc[T]) Routers() []st.IRouter { return nil }
func (s *testManageSvc[T]) ParseBefore(interface{}, st.IRequest) error  { return nil }
func (s *testManageSvc[T]) ParseAfter(interface{}, st.IRequest) error   { return nil }
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
func (s *testManageSvc[T]) GetView() *view.ViewModel                  { return nil }
func (s *testManageSvc[T]) ViewModel(*view.ViewModel)                  {}
func (s *testManageSvc[T]) ViewFieldModel(interface{}, *view.FieldModel) {}
func (s *testManageSvc[T]) ViewCommandModel(*view.CommandModel)         {}
func (s *testManageSvc[T]) ViewChildModel(*view.ViewChildModel)         {}

// IGetModelList
func (s *testManageSvc[T]) GetList() interface{} { return s.cachedList }

// mockDataAction captures Insert calls and returns nil errors.
type mockDataAction struct {
	inserted []interface{}
}

func (m *mockDataAction) Transaction() error                            { return nil }
func (m *mockDataAction) Load(*pt.SearchItem, interface{}) error        { return nil }
func (m *mockDataAction) Insert(data interface{}) error                 { m.inserted = append(m.inserted, data); return nil }
func (m *mockDataAction) Update(interface{}) error                      { return nil }
func (m *mockDataAction) Delete(interface{}) error                      { return nil }
func (m *mockDataAction) Raw(string, interface{}) error                 { return nil }
func (m *mockDataAction) Exec(string, interface{}) error                { return nil }
func (m *mockDataAction) GetModelDB(interface{}) (interface{}, error)   { return nil, nil }
func (m *mockDataAction) Commit() error                                 { return nil }
func (m *mockDataAction) GetRunDB() interface{}                         { return nil }
func (m *mockDataAction) Rollback() error                               { return nil }

// crudRequest is a minimal IRequest for CRUD operation tests.
type crudRequest struct {
	body interface{}
}

func (r *crudRequest) ServiceName() string                                                    { return "crud-test-svc" }
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
func (r *crudRequest) GoZeroBind(interface{}) error                                          { return nil }
func (r *crudRequest) GetTraceId() string                                                    { return "" }
func (r *crudRequest) GetUser() (string, string)                                             { return "", "" }
func (r *crudRequest) GetClientIP() string                                                   { return "127.0.0.1" }
func (r *crudRequest) NewID() uint                                                           { return 1 }
func (r *crudRequest) Authorized() bool                                                      { return true }
func (r *crudRequest) GetValue(string) string                                                { return "" }
func (r *crudRequest) NewResponse(interface{}, error) st.IResponse                          { return nil }
func (r *crudRequest) GetPath() string                                                       { return "" }
func (r *crudRequest) GetClaims(string) interface{}                                         { return nil }
func (r *crudRequest) GetServerInfo() *st.TargetInfo                                        { return nil }
func (r *crudRequest) GetTargetServerInfo(string) *st.TargetInfo                            { return nil }
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
