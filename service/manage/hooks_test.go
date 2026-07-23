package manage_test

import (
	"testing"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

type hookedManageOwner struct {
	*manage.HookedManageService[testItem]
	addBeforeCalled     bool
	doBeforeSender      interface{}
	searchAfterCalled   bool
	searchAfterRowCount int
}

func newHookedManageOwner() *hookedManageOwner {
	own := &hookedManageOwner{}
	own.HookedManageService = manage.NewHookedManageService[testItem](own)
	return own
}

func (own *hookedManageOwner) OnAddBefore(_ *manage.Add[testItem], _ servertypes.IRequest) (interface{}, error, bool) {
	own.addBeforeCalled = true
	return "hook-add-before", nil, true
}

func (own *hookedManageOwner) OnDoBefore(sender interface{}, _ servertypes.IRequest) (interface{}, error, bool) {
	own.doBeforeSender = sender
	return nil, nil, false
}

func (own *hookedManageOwner) OnSearchAfter(_ *manage.Search[testItem], result *view.TableData, _ servertypes.IRequest) (interface{}, error) {
	own.searchAfterCalled = true
	if result != nil {
		own.searchAfterRowCount = int(result.Total)
		result.Tag = "hook-search-after"
	}
	return result, nil
}

func TestHookedManageServiceDispatchesFineGrainedHooks(t *testing.T) {
	own := newHookedManageOwner()
	req := &crudRequest{}

	result, err, stop := own.DoBefore(own.Add, req)
	require.NoError(t, err)
	require.True(t, stop)
	require.Equal(t, "hook-add-before", result)
	require.True(t, own.addBeforeCalled)

	table := &view.TableData{Rows: []*testItem{{Name: "item"}}, Total: 1}
	after, err := own.SearchAfter(own.Search, table, req)
	require.NoError(t, err)
	require.Same(t, table, after)
	require.True(t, own.searchAfterCalled)
	require.Equal(t, 1, own.searchAfterRowCount)
	require.Equal(t, "hook-search-after", table.Tag)
}

func TestHookedManageServiceCallsOnDoBeforeForCustomSender(t *testing.T) {
	own := newHookedManageOwner()
	req := &crudRequest{}
	customCommand := struct{ name string }{name: "set-enabled"}

	result, err, stop := own.DoBefore(customCommand, req)
	require.NoError(t, err)
	require.False(t, stop)
	require.Nil(t, result)
	require.Equal(t, customCommand, own.doBeforeSender)
}

func TestHookedManageServiceDefaultsAreNoops(t *testing.T) {
	own := manage.NewHookedManageService[testItem](nil)
	req := &crudRequest{}

	result, err, stop := own.DoBefore(own.Add, req)
	require.NoError(t, err)
	require.False(t, stop)
	require.Nil(t, result)

	table := &view.TableData{Rows: []*testItem{{Name: "item"}}, Total: 1}
	after, err := own.SearchAfter(own.Search, table, req)
	require.NoError(t, err)
	require.Same(t, table, after)
}
