package view

// 本文件冻结 Manage ViewModel 的 JSON 字段名，供前端 manage-protocol 对齐。

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// jsonFieldNames 收集结构体 json tag 名（跳过 "-" 与空 tag）。
func jsonFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	seen := map[string]struct{}{}
	var walk func(reflect.Type)
	walk = func(current reflect.Type) {
		if current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < current.NumField(); i++ {
			field := current.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("json")
			if tag == "-" {
				continue
			}
			if field.Anonymous && (tag == "" || tag == ",inline") {
				walk(field.Type)
				continue
			}
			name := field.Name
			if tag != "" {
				if comma := indexComma(tag); comma >= 0 {
					tag = tag[:comma]
				}
				if tag != "" {
					name = tag
				}
			}
			seen[name] = struct{}{}
		}
	}
	walk(typ)
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func indexComma(tag string) int {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return i
		}
	}
	return -1
}

func TestViewModelJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(ViewModel{}))
	want := []string{
		"autoload", "autoSearch", "childmodels", "commands", "desc",
		"disabled", "fields", "name", "servicename", "showComvtp",
		"title", "viewtype", "visible",
	}
	assertStringSlice(t, want, got)
}

func TestFieldModelJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(FieldModel{}))
	want := []string{
		"comvtp", "datatimetype", "defaultvalue", "disabled", "field",
		"foreign", "index", "isedit", "iskey", "ispassword", "isremark",
		"issearch", "length", "min", "porpfield", "posttype", "precision",
		"required", "showInComvtp", "sorter", "tag", "title", "type", "visible",
	}
	assertStringSlice(t, want, got)
}

func TestDataTimeTypeJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(DataTimeTypeModel{}))
	want := []string{"dateformat", "isdate", "istime", "isutc", "timeformat"}
	assertStringSlice(t, want, got)
}

func TestForeignModelJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(ForeignModel{}))
	want := []string{
		"isfkey", "manydisplayfield", "manyobjectfield", "manyobjectfieldkey",
		"manyobjectname", "manyobjecttypename", "mapitems", "model",
		"onedisplayname", "oneobjectfield", "oneobjectfieldkey",
		"oneobjectforeignkeyvalue", "oneobjectname", "oneobjecttypename",
	}
	assertStringSlice(t, want, got)
}

func TestSearchItemJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(SearchItem{}))
	want := []string{
		"childmodel", "field", "foreign", "page", "parent", "size",
		"sortList", "tag", "value", "whereList",
	}
	assertStringSlice(t, want, got)
}

func TestSearchSortJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(SearchSort{}))
	want := []string{"isdesc", "name"}
	assertStringSlice(t, want, got)
}

func TestTableDataJSONTags(t *testing.T) {
	got := jsonFieldNames(t, reflect.TypeOf(TableData{}))
	want := []string{"rows", "tag", "total"}
	assertStringSlice(t, want, got)
}

func TestViewModelRoundTripKeepsFrozenNames(t *testing.T) {
	raw, err := json.Marshal(FieldModel{PropField: "Code"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["porpfield"]; !ok {
		t.Fatalf("missing frozen json name porpfield in %s", raw)
	}
	if _, ok := payload["propfield"]; ok {
		t.Fatalf("unexpected corrected name propfield in %s", raw)
	}
	dt := DataTimeTypeModel{IsDate: true}
	raw, err = json.Marshal(dt)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["isdate"]; !ok {
		t.Fatalf("missing isdate in %s", raw)
	}
	if _, ok := payload["isdata"]; ok {
		t.Fatalf("unexpected isdata in %s", raw)
	}
}

func assertStringSlice(t *testing.T, want, got []string) {
	t.Helper()
	want = append([]string(nil), want...)
	sort.Strings(want)
	if len(want) != len(got) {
		t.Fatalf("len want=%d got=%d\nwant=%v\ngot=%v", len(want), len(got), want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("index %d: want %q got %q\nwant=%v\ngot=%v", i, want[i], got[i], want, got)
		}
	}
}
