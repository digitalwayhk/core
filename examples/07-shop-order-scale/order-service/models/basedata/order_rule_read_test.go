// 本文件验证规则点查不执行分页总数查询，同时保持不存在与读取失败的区别。
package basedata

import (
	"errors"
	"testing"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

type ruleReadAction struct {
	persistencetypes.IDataAction
	loadErr error
}

func (a ruleReadAction) Load(query *persistencetypes.SearchItem, result interface{}) error {
	if !query.SkipCount {
		return errors.New("规则点查不应执行 COUNT")
	}
	return a.loadErr
}

// TestEnabledRuleRead 验证不存在使用显式错误，数据库失败不会被当成默认规则。
func TestEnabledRuleRead(t *testing.T) {
	_, err := GetEnabledOrderRuleWith(ruleReadAction{}, "default")
	require.ErrorIs(t, err, ErrOrderRuleNotFound)
	want := errors.New("数据库不可用")
	_, err = GetEnabledOrderRuleWith(ruleReadAction{loadErr: want}, "default")
	require.ErrorIs(t, err, want)
}
