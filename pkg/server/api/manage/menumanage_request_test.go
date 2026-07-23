package manage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"sync"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ manageservice.IGetDefaultItemsWithRequest[smodels.MenuModel] = (*MenuManage)(nil)

type menuRequest struct {
	types.IRequest
	id uint
}

func (r *menuRequest) NewID() uint { return r.id }

func TestMenuManageRequestIsolation(t *testing.T) {
	menu := NewMenuManage()
	serviceContext := &router.ServiceContext{
		Service: &types.Service{Name: "request-isolation"},
	}
	requests := []*menuRequest{
		{id: 501},
		{id: 502},
	}

	ids := make(chan uint, len(requests))
	var wg sync.WaitGroup
	for _, req := range requests {
		wg.Add(1)
		go func(req types.IRequest) {
			defer wg.Done()
			ids <- menu.newDirectoryModel(req, serviceContext).ID
		}(req)
	}
	wg.Wait()
	close(ids)

	actual := make([]uint, 0, len(requests))
	for id := range ids {
		actual = append(actual, id)
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i] < actual[j] })
	assert.Equal(t, []uint{501, 502}, actual)
}

func TestMenuManageDoesNotSetSharedRequest(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "menumanage.go", nil, 0)
	require.NoError(t, err)

	var calls []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "SetReq" {
			calls = append(calls, fset.Position(call.Pos()).String())
		}
		return true
	})

	assert.Empty(t, calls, "MenuManage must pass req explicitly instead of calling SetReq: %v", calls)
}
