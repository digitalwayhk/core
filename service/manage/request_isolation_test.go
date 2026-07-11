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

func TestSearchAfterDefaultItemsLegacyFallback(t *testing.T) {
	defaults := &legacyDefaults{}
	svc := manage.NewManageService[testItem](defaults)

	_, err := svc.SearchAfter(nil, &view.TableData{}, &requestWithID{
		crudRequest: &crudRequest{},
		id:          303,
	})
	require.NoError(t, err)

	defaults.mu.Lock()
	defer defaults.mu.Unlock()
	assert.Equal(t, 1, defaults.calls)
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
