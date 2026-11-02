// 本文件锁定固定模式公开JSON字节及指纹，不允许动态入口悄悄修改旧合同。
package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFixedManifestJSONAndFingerprintRemainExact(t *testing.T) {
	m := Manifest{Scope: "scope", Service: "service", Group: "group", Selector: "v1", Subjects: []string{"b", "a"}, Keys: []string{"B", "A"}}.Normalize()
	// v1.7.1公开结构及顺序，不能用新结构的别名构造比较值。
	legacy := struct {
		Scope, Service, Group, Selector string
		Subjects, Keys                  []string
		Window                          int64
		MaxBatch, MaxReferences         int
		MaxMessageBytes                 int64
		Timing                          Timing
	}{m.Scope, m.Service, m.Group, m.Selector, m.Subjects, m.Keys, m.Window, m.MaxBatch, m.MaxReferences, m.MaxMessageBytes, m.Timing}
	old, err := json.Marshal(legacy)
	require.NoError(t, err)
	now, err := json.Marshal(m)
	require.NoError(t, err)
	require.Equal(t, string(old), string(now))
	sum := sha256.Sum256(old)
	require.Equal(t, hex.EncodeToString(sum[:]), m.Fingerprint())
}
func TestDynamicManifestExplicitDefaultsAndIsolation(t *testing.T) {
	m := Manifest{Scope: "s", Service: "s", Group: "s", Selector: "v1", Subjects: []string{"one"}, DynamicKeys: &DynamicKeyPolicy{}}.Normalize()
	require.NoError(t, m.Validate())
	require.Equal(t, 256, m.DynamicKeys.MaxKeys)
	require.Equal(t, 32, m.DynamicKeys.PageSize)
	require.Equal(t, m.Window, m.DynamicKeys.UnregisteredWindow)
	invalid := m
	invalid.Keys = []string{"A"}
	require.Error(t, invalid.Validate())
	changed := m.Normalize()
	changed.DynamicKeys.UnregisteredWindow++
	require.NotEqual(t, m.Fingerprint(), changed.Fingerprint())
	require.EqualValues(t, 16, m.PendingWindow())
}
