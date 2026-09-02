package manage_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestWithID struct {
	*crudRequest
	id uint
}

func (r *requestWithID) NewID() uint { return r.id }

type requestAwareDefaults struct {
	mu          sync.Mutex
	requestIDs  []uint
	legacyCalls int
}

func (d *requestAwareDefaults) GetDefaultItemsWithRequest(req st.IRequest) []*testItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requestIDs = append(d.requestIDs, req.NewID())
	return nil
}

func (d *requestAwareDefaults) GetDefaultItems() []*testItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.legacyCalls++
	return nil
}

func (d *requestAwareDefaults) snapshot() ([]uint, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]uint(nil), d.requestIDs...), d.legacyCalls
}

func TestSearchAfterRequestIsolationPrefersRequestAwareDefaultItems(t *testing.T) {
	defaults := &requestAwareDefaults{}
	svc := manage.NewManageService[testItem](defaults)
	requests := []st.IRequest{
		&requestWithID{crudRequest: &crudRequest{}, id: 101},
		&requestWithID{crudRequest: &crudRequest{}, id: 202},
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(requests))
	for _, req := range requests {
		wg.Add(1)
		go func(req st.IRequest) {
			defer wg.Done()
			_, err := svc.SearchAfter(nil, &view.TableData{}, req)
			errs <- err
		}(req)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	requestIDs, legacyCalls := defaults.snapshot()
	sort.Slice(requestIDs, func(i, j int) bool { return requestIDs[i] < requestIDs[j] })
	assert.Equal(t, []uint{101, 202}, requestIDs)
	assert.Zero(t, legacyCalls, "request-aware defaults must take precedence over the legacy hook")
}

type legacyDefaults struct {
	mu    sync.Mutex
	calls int
}

func (d *legacyDefaults) GetDefaultItems() []*testItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return nil
}

type defaultsWithResolvedList struct {
	*manage.ManageService[testItem]
	list *entity.ModelList[testItem]
}

func newDefaultsWithResolvedList(action *mockDataAction) *defaultsWithResolvedList {
	owner := &defaultsWithResolvedList{
		list: entity.NewModelList[testItem](action),
	}
	owner.ManageService = manage.NewManageService[testItem](owner)
	owner.Search.New(owner)
	return owner
}

func (d *defaultsWithResolvedList) GetList() interface{} {
	return d.list
}

func (d *defaultsWithResolvedList) GetDefaultItems() []*testItem {
	return []*testItem{{Model: &entity.Model{}, Name: "default"}}
}

func TestSearchAfterUsesResolvedSearchModelList(t *testing.T) {
	action := &mockDataAction{}
	owner := newDefaultsWithResolvedList(action)
	owner.Search.SearchItem = &view.SearchItem{Page: 1, Size: 10}
	result := &view.TableData{}

	actual, err := owner.SearchAfter(owner.Search, result, &crudRequest{})
	require.NoError(t, err)
	require.Same(t, result, actual)
	require.Len(t, action.inserted, 1)
	assert.Equal(t, int64(1), result.Total)
}

func TestSearchAfterDefaultItemsLegacyFallback(t *testing.T) {
	defaults := &legacyDefaults{}
	svc := manage.NewManageService[testItem](defaults)
	sender := manage.NewSearch[testItem](defaults)
	sender.SearchItem = &view.SearchItem{Page: 1, Size: 10}

	_, err := svc.SearchAfter(sender, &view.TableData{}, &requestWithID{
		crudRequest: &crudRequest{},
		id:          303,
	})
	require.NoError(t, err)

	defaults.mu.Lock()
	defer defaults.mu.Unlock()
	assert.Equal(t, 1, defaults.calls)
}

// TestSearchAfterZeroPageBootstrapsDefaults 验证尚未归一化的 Page=0 仍按
// 第一页处理；只有 Page>1 才属于后续分页。
func TestSearchAfterZeroPageBootstrapsDefaults(t *testing.T) {
	defaults := &legacyDefaults{}
	svc := manage.NewManageService[testItem](defaults)
	sender := manage.NewSearch[testItem](defaults)
	sender.SearchItem = &view.SearchItem{Page: 0, Size: 10}

	_, err := svc.SearchAfter(sender, &view.TableData{}, &requestWithID{
		crudRequest: &crudRequest{},
		id:          306,
	})
	require.NoError(t, err)

	defaults.mu.Lock()
	defer defaults.mu.Unlock()
	assert.Equal(t, 1, defaults.calls)
}

// TestSearchAfterFilteredEmptyResultDoesNotBootstrapDefaults 验证带筛选条件的
// 空结果就是诚实的空列表，不得触发仅用于首次空表初始化的默认数据写入。
func TestSearchAfterFilteredEmptyResultDoesNotBootstrapDefaults(t *testing.T) {
	defaults := &legacyDefaults{}
	svc := manage.NewManageService[testItem](defaults)
	sender := manage.NewSearch[testItem](defaults)
	sender.SearchItem = &view.SearchItem{
		Page: 1,
		Size: 10,
		WhereList: []*view.SearchWhere{{
			Name:  "Name",
			Value: "deleted-menu",
		}},
	}
	empty := &view.TableData{Rows: []*testItem{}, Total: 0}

	result, err := svc.SearchAfter(sender, empty, &requestWithID{
		crudRequest: &crudRequest{},
		id:          304,
	})
	require.NoError(t, err)
	require.Same(t, empty, result)

	defaults.mu.Lock()
	defer defaults.mu.Unlock()
	assert.Zero(t, defaults.calls)
	assert.Zero(t, empty.Total)
	assert.Empty(t, empty.Rows)
}

// TestSearchAfterLaterPageEmptyResultDoesNotBootstrapDefaults 验证后续分页的
// 空结果不会触发默认数据初始化；初始化只允许发生在无筛选的第一页。
func TestSearchAfterLaterPageEmptyResultDoesNotBootstrapDefaults(t *testing.T) {
	defaults := &legacyDefaults{}
	svc := manage.NewManageService[testItem](defaults)
	sender := manage.NewSearch[testItem](defaults)
	sender.SearchItem = &view.SearchItem{Page: 2, Size: 10}
	empty := &view.TableData{Rows: []*testItem{}, Total: 0}

	result, err := svc.SearchAfter(sender, empty, &requestWithID{
		crudRequest: &crudRequest{},
		id:          305,
	})
	require.NoError(t, err)
	require.Same(t, empty, result)

	defaults.mu.Lock()
	defer defaults.mu.Unlock()
	assert.Zero(t, defaults.calls)
	assert.Zero(t, empty.Total)
	assert.Empty(t, empty.Rows)
}

func TestManageServiceLegacyRequestAPIStillCompiles(t *testing.T) {
	svc := manage.NewManageService[testItem](nil)
	req := &requestWithID{crudRequest: &crudRequest{}, id: 404}

	var setter manage.IRequestSet = svc
	setter.SetReq(req)

	require.Same(t, req, svc.Req)
	t.Log("Deprecated: 依赖 Req/SetReq 隐式副作用的业务扩展必须迁移到显式 req 参数")
}

func TestManageRoutesDoNotCallSetReq(t *testing.T) {
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		return strings.HasSuffix(info.Name(), ".go") && !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	require.NoError(t, err)

	var calls []string
	for _, pkg := range packages {
		for filename, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "SetReq" {
					calls = append(calls, fset.Position(call.Pos()).String()+" in "+filename)
				}
				return true
			})
		}
	}

	assert.Empty(t, calls, "framework routes must not persist request state: %v", calls)
}
