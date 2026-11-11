// 本文件以原JSON往返为独立oracle，验证快照复用资格、深拷贝、规范字节和兼容回退。
package nosql

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	corejson "github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

type snapshotPlainDetail struct {
	Label  string
	Values []int
}
type snapshotPlainRecord struct {
	*entity.Model
	Key        string
	Payload    []byte
	Optional   []string `json:",omitempty"`
	NilSlice   *[]int
	NilPointer **int
	Nested     *snapshotPlainDetail
	Text       string
	Number     float64
	Ignored    string `json:"-"`
}

// GetLocalKey为合成模型提供稳定身份，不注入业务概念。
func (r *snapshotPlainRecord) GetLocalKey() string { return r.Key }

type snapshotNormalizingValue string

// MarshalJSON保持调用方原文本；解码后规范化故意与原输入不同。
func (v snapshotNormalizingValue) MarshalJSON() ([]byte, error) { return json.Marshal(string(v)) }

// UnmarshalJSON用于验证应用自定义解码必须继续执行，不能被新快照复用绕过。
func (v *snapshotNormalizingValue) UnmarshalJSON(b []byte) error {
	var s string
	if e := json.Unmarshal(b, &s); e != nil {
		return e
	}
	*v = snapshotNormalizingValue(strings.ToUpper(strings.TrimSpace(s)))
	return nil
}

type snapshotCodecRecord struct {
	*entity.Model
	Key   string
	Value snapshotNormalizingValue
}

// GetLocalKey给自定义codec夹具提供稳定键。
func (r *snapshotCodecRecord) GetLocalKey() string { return r.Key }

type snapshotCodecTarget struct{}

// SyncBatch仅确认本次合成原件。
func (snapshotCodecTarget) SyncBatch(_ context.Context, items []*SyncQueueItem[snapshotCodecRecord]) (*WriteBehindResult, error) {
	r := &WriteBehindResult{}
	for _, i := range items {
		r.ConfirmedKeys = append(r.ConfirmedKeys, i.Key)
	}
	return r, nil
}

// OrderingGroup使用固定组，保留原自定义codec路径。
func (snapshotCodecTarget) OrderingGroup(*SyncQueueItem[snapshotCodecRecord]) (string, error) {
	return "codec", nil
}

// HandleFailure显式暂停，不能以优化绕过原件失败责任。
func (snapshotCodecTarget) HandleFailure(_ context.Context, f WriteBehindFailure[snapshotCodecRecord]) (WriteBehindDecision, error) {
	return WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey}, nil
}

// TestTransactionSnapshotReuseCustomCodecFallback保留规范化codec及原指纹，不能直接hash前台编码。
func TestTransactionSnapshotReuseCustomCodecFallback(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.WriteBehindFailure = failureTestPolicy()
	store, _, err := NewReliableWriteStore[snapshotCodecRecord](ServiceIdentity{ServiceName: "codec"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	require.NoError(t, store.UseWriteBehindWithFailureHandler(snapshotCodecTarget{}, snapshotCodecTarget{}))
	reads := 0
	store.db.disposition.snapshotReadObserver = func() { reads++ }
	value := &snapshotCodecRecord{Model: entity.NewModel(), Key: "key", Value: "  lower  "}
	require.NoError(t, store.Save(context.Background(), value))
	require.Equal(t, 1, reads)
	records, _, err := store.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, snapshotNormalizingValue("LOWER"), records[0].Snapshot.Item.Value)
	canonical, err := json.Marshal(records[0].Snapshot)
	require.NoError(t, err)
	require.Equal(t, dispositionHash(string(canonical)), records[0].Fingerprint)
	require.Equal(t, snapshotNormalizingValue("  lower  "), value.Value)
}

// TestTransactionSnapshotReuseMatchesJSONRoundtrip包含无效UTF8、空切片、指针、负零和带时区time的旧往返语义。
func TestTransactionSnapshotReuseMatchesJSONRoundtrip(t *testing.T) {
	fast := 0
	for _, number := range []float64{1.25, 1e-9, math.Copysign(0, -1)} {
		for _, text := range []string{"ordinary", string([]byte{0xff, 0xff, 'a'})} {
			for _, zone := range []*time.Location{time.UTC, time.FixedZone("fixture", 5*3600+1800), time.Local} {
				now := time.Now().In(zone)
				item := &snapshotPlainRecord{Model: entity.NewModel(), Key: "roundtrip", Payload: []byte{0, 1, 255}, Optional: []string{}, Nested: &snapshotPlainDetail{Label: "nested", Values: []int{1, 2}}, Text: text, Number: number, Ignored: "must disappear"}
				var nilSlice []int
				var nilPointer *int
				item.NilSlice = &nilSlice
				item.NilPointer = &nilPointer
				item.CreatedAt = &now
				original := &SyncQueueItem[snapshotPlainRecord]{Key: "roundtrip", Item: item, CreatedAt: now, UpdatedAt: now, Op: OpInsert, fromSyncQueue: true}
				raw, err := corejson.Marshal(original)
				require.NoError(t, err)
				var expected SyncQueueItem[snapshotPlainRecord]
				require.NoError(t, json.Unmarshal(raw, &expected))
				db := &PrefixedBadgerDB[snapshotPlainRecord]{disposition: &writeBehindDispositionRuntime[snapshotPlainRecord]{snapshotCopy: dispositionCopyPlan(reflect.TypeFor[snapshotPlainRecord](), map[reflect.Type]bool{})}}
				want, err := json.Marshal(&expected)
				require.NoError(t, err)
				prepared := db.copyNewDispositionSnapshot(original, raw)
				if !bytes.Equal(raw, want) {
					require.Nil(t, prepared, "双JSON字节不等时必须回退，不能直接hash前台字节")
					continue
				}
				fast++
				require.NotNil(t, prepared)
				actual := prepared.snapshot
				require.NotNil(t, actual)
				require.Equal(t, expected, *actual)
				got, err := json.Marshal(actual)
				require.NoError(t, err)
				require.Equal(t, string(want), string(got))
				changed := append([]byte(" "), raw...)
				require.Nil(t, db.copyNewDispositionSnapshot(original, changed))
				item.Payload[0] = 9
				item.Nested.Values[0] = 9
				item.CreatedAt = nil
				original.UpdatedAt = time.Time{}
				require.Equal(t, byte(0), actual.Item.Payload[0])
				require.Equal(t, 1, actual.Item.Nested.Values[0])
				require.NotNil(t, actual.Item.CreatedAt)
				require.False(t, actual.UpdatedAt.IsZero())
			}
		}
	}
	require.Positive(t, fast, "不能全部回退却声称复用通过")
}

// SnapshotOmittedFields验证匿名导出指针全省略时的nil恢复语义。
type SnapshotOmittedFields struct {
	Value string `json:",omitempty"`
}
type snapshotOmittedEmbedding struct{ *SnapshotOmittedFields }

type snapshotRecursive struct{ *snapshotRecursive }
type snapshotInterface struct{ Value any }
type snapshotMap struct{ Value map[string]int }

// TestTransactionSnapshotReuseConservativeEligibility不能证明字段选择/动态codec等价时必须走原路径。
func TestTransactionSnapshotReuseConservativeEligibility(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[snapshotOmittedEmbedding](), reflect.StructOf([]reflect.StructField{{Name: "A", Type: reflect.TypeFor[string](), Tag: `json:"x"`}, {Name: "B", Type: reflect.TypeFor[string](), Tag: `json:"x"`}}), reflect.TypeFor[snapshotRecursive](), reflect.TypeFor[snapshotInterface](), reflect.TypeFor[snapshotMap](), reflect.TypeFor[snapshotCodecRecord]()} {
		require.Nil(t, dispositionCopyPlan(typ, map[reflect.Type]bool{}), typ.String())
	}
}

// snapshotPlainTarget持有OrderingGroup看到的快照，固定同批后续Save不得改变它。
type snapshotPlainTarget struct {
	snapshots []*SyncQueueItem[snapshotPlainRecord]
}

// SyncBatch仅确认合成原件，不连接任何远端环境。
func (*snapshotPlainTarget) SyncBatch(_ context.Context, items []*SyncQueueItem[snapshotPlainRecord]) (*WriteBehindResult, error) {
	r := &WriteBehindResult{}
	for _, v := range items {
		r.ConfirmedKeys = append(r.ConfirmedKeys, v.Key)
	}
	return r, nil
}

// OrderingGroup记录原独立快照，不解析业务身份。
func (a *snapshotPlainTarget) OrderingGroup(item *SyncQueueItem[snapshotPlainRecord]) (string, error) {
	a.snapshots = append(a.snapshots, item)
	return item.Item.Nested.Label, nil
}

// HandleFailure保持显式暂停语义。
func (*snapshotPlainTarget) HandleFailure(_ context.Context, f WriteBehindFailure[snapshotPlainRecord]) (WriteBehindDecision, error) {
	return WriteBehindDecision{ID: f.Record.ID, Version: f.Record.Snapshot.UpdatedAt, Fingerprint: f.Record.Fingerprint, Action: WriteBehindPause, Scope: WriteBehindScopeKey}, nil
}

// TestTransactionSnapshotReuseCallbackIsolation经实际SaveBatch验证同key多次写/改组的回调快照不会被后续写覆盖。
func TestTransactionSnapshotReuseCallbackIsolation(t *testing.T) {
	path := t.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.WriteBehindFailure = failureTestPolicy()
	store, _, err := NewReliableWriteStore[snapshotPlainRecord](ServiceIdentity{ServiceName: "snapshot"}, ReliableWriteStoreConfig{BasePath: path, Badger: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	target := &snapshotPlainTarget{}
	require.NoError(t, store.UseWriteBehindWithFailureHandler(target, target))
	items := []*snapshotPlainRecord{}
	for _, group := range []string{"first", "second"} {
		items = append(items, &snapshotPlainRecord{Model: entity.NewModel(), Key: "same", Payload: []byte(group), Nested: &snapshotPlainDetail{Label: group, Values: []int{1, 2}}})
	}
	reads := 0
	store.db.disposition.snapshotReadObserver = func() { reads++ }
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 2, result.Committed)
	require.Zero(t, reads)
	require.Len(t, target.snapshots, 2)
	require.Equal(t, "first", string(target.snapshots[0].Item.Payload))
	require.Equal(t, "second", string(target.snapshots[1].Item.Payload))
	items[0].Payload[0] = 'X'
	items[1].Nested.Values[0] = 9
	require.Equal(t, "first", string(target.snapshots[0].Item.Payload))
	require.Equal(t, 1, target.snapshots[1].Item.Nested.Values[0])
	require.Equal(t, 1, store.Metrics().Pending)
}
