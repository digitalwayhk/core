// 本文件仅为当前事务的新wrapper生成独立快照；不能证明JSON往返等价的模型继续使用原始回读路径。
package nosql

import (
	"bytes"
	"encoding"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type dispositionPreparedSnapshot[T types.IModel] struct {
	snapshot  *SyncQueueItem[T]
	canonical []byte
}

type dispositionValueCopy func(reflect.Value) (reflect.Value, error)

// dispositionCopyPlan仅在绑定时构造；不缓存业务对象，不支持自定义codec/动态类型或递归模型。
func dispositionCopyPlan(t reflect.Type, visiting map[reflect.Type]bool) dispositionValueCopy {
	if t == reflect.TypeFor[time.Time]() {
		return func(v reflect.Value) (reflect.Value, error) {
			data, err := v.Interface().(time.Time).MarshalJSON()
			if err != nil {
				return reflect.Value{}, err
			}
			var value time.Time
			err = value.UnmarshalJSON(data)
			return reflect.ValueOf(value), err
		}
	}
	for _, contract := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[json.Unmarshaler](), reflect.TypeFor[encoding.TextMarshaler](), reflect.TypeFor[encoding.TextUnmarshaler]()} {
		if t != reflect.TypeFor[*time.Time]() && (t.Implements(contract) || reflect.PointerTo(t).Implements(contract)) {
			return nil
		}
	}
	if visiting[t] {
		return nil
	}
	visiting[t] = true
	defer delete(visiting, t)
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return func(v reflect.Value) (reflect.Value, error) { return v, nil }
	case reflect.String:
		return func(v reflect.Value) (reflect.Value, error) {
			if utf8.ValidString(v.String()) {
				return v, nil
			}
			r := reflect.New(t).Elem()
			r.SetString(string([]rune(v.String())))
			return r, nil
		}
	case reflect.Pointer:
		if t != reflect.PointerTo(t.Elem()) {
			return nil
		}
		copyElement := dispositionCopyPlan(t.Elem(), visiting)
		if copyElement == nil {
			return nil
		}
		return func(v reflect.Value) (reflect.Value, error) {
			// JSON null会把整条指针链解码为nil，包括指向nil切片的非nil指针。
			for value := v; value.Kind() == reflect.Pointer || value.Kind() == reflect.Slice; {
				if value.IsNil() {
					return reflect.Zero(t), nil
				}
				if value.Kind() == reflect.Slice {
					break
				}
				value = value.Elem()
			}
			x, e := copyElement(v.Elem())
			if e != nil {
				return reflect.Value{}, e
			}
			r := reflect.New(t.Elem())
			r.Elem().Set(x)
			return r, nil
		}
	case reflect.Slice, reflect.Array:
		copyElement := dispositionCopyPlan(t.Elem(), visiting)
		if copyElement == nil {
			return nil
		}
		return func(v reflect.Value) (reflect.Value, error) {
			if t.Kind() == reflect.Slice && v.IsNil() {
				return reflect.Zero(t), nil
			}
			var r reflect.Value
			if t.Kind() == reflect.Slice {
				r = reflect.MakeSlice(t, v.Len(), v.Len())
			} else {
				r = reflect.New(t).Elem()
			}
			if t.Elem().Kind() == reflect.Uint8 {
				reflect.Copy(r, v)
				return r, nil
			}
			for i := range v.Len() {
				x, e := copyElement(v.Index(i))
				if e != nil {
					return reflect.Value{}, e
				}
				r.Index(i).Set(x)
			}
			return r, nil
		}
	case reflect.Struct:
		// 冲突/匿名遮蔽的字段选择仍由原JSON路径处理，不自行重实现JSON字段解析规则。
		if !dispositionUniqueJSONFields(t, map[string]bool{}, map[reflect.Type]bool{}) {
			return nil
		}
		type fieldCopy struct {
			index int
			empty bool
			copy  dispositionValueCopy
		}
		fields := []fieldCopy{}
		for i := range t.NumField() {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")
			if f.PkgPath != "" || tag[0] == "-" {
				continue
			}
			empty := false
			for _, option := range tag[1:] {
				switch option {
				case "omitempty":
					empty = true
				case "", "string":
				default:
					return nil
				}
			}
			if f.Anonymous && tag[0] == "" && f.Type.Kind() == reflect.Pointer {
				// 空匿名指针的全部成员若被省略，JSON解码不会分配该指针，不能照搬非nil值。
				required := false
				for j := range f.Type.Elem().NumField() {
					nested := f.Type.Elem().Field(j)
					options := strings.Split(nested.Tag.Get("json"), ",")
					if nested.PkgPath == "" && !nested.Anonymous && options[0] != "-" && !strings.Contains(nested.Tag.Get("json"), "omitempty") {
						required = true
						break
					}
				}
				if !required {
					return nil
				}
			}
			copyField := dispositionCopyPlan(f.Type, visiting)
			if copyField == nil {
				return nil
			}
			fields = append(fields, fieldCopy{i, empty, copyField})
		}
		return func(v reflect.Value) (reflect.Value, error) {
			r := reflect.New(t).Elem()
			for _, f := range fields {
				x := v.Field(f.index)
				if f.empty && dispositionJSONEmpty(x) {
					continue
				}
				y, e := f.copy(x)
				if e != nil {
					return reflect.Value{}, e
				}
				r.Field(f.index).Set(y)
			}
			return r, nil
		}
	}
	return nil
}

func dispositionUniqueJSONFields(t reflect.Type, names map[string]bool, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	defer delete(seen, t)
	for i := range t.NumField() {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if f.PkgPath != "" {
			if f.Anonymous {
				return false
			}
			continue
		}
		if f.Anonymous && name == "" {
			nested := f.Type
			if nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if nested.Kind() == reflect.Struct {
				if nested == t || !dispositionUniqueJSONFields(nested, names, seen) {
					return false
				}
				continue
			}
			return false
		}
		if name == "" {
			name = f.Name
		}
		name = strings.ToLower(name)
		if names[name] {
			return false
		}
		names[name] = true
	}
	return true
}

func dispositionJSONEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

// copyNewDispositionSnapshot只接受本次ApplyWriteOperations持有的新版本；旧原件/恢复入口仍读事务权威数据。
func (p *PrefixedBadgerDB[T]) copyNewDispositionSnapshot(wrapper *SyncQueueItem[T], written []byte) *dispositionPreparedSnapshot[T] {
	if p.disposition == nil || p.disposition.snapshotCopy == nil || wrapper.Item == nil {
		return nil
	}
	value, err := p.disposition.snapshotCopy(reflect.ValueOf(wrapper.Item).Elem())
	if err != nil {
		return nil
	}
	snapshot := *wrapper
	item := new(T)
	reflect.ValueOf(item).Elem().Set(value)
	snapshot.Item = item
	snapshot.fromSyncQueue = false
	// 与原JSON解码一致地去除monotonic clock并恢复时区表示，不把内存time值直接当持久快照。
	for _, field := range []*time.Time{&snapshot.CreatedAt, &snapshot.UpdatedAt, &snapshot.SyncedAt, &snapshot.DeletedAt} {
		data, e := field.MarshalJSON()
		if e != nil {
			return nil
		}
		if e = field.UnmarshalJSON(data); e != nil {
			return nil
		}
	}
	canonical, err := json.Marshal(&snapshot)
	if err != nil || !bytes.Equal(written, canonical) {
		return nil
	}
	return &dispositionPreparedSnapshot[T]{snapshot: &snapshot, canonical: canonical}
}
