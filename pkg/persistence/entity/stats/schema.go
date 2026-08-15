package stats

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// fieldMeta 描述可被统计引用的导出字段。
type fieldMeta struct {
	GoName   string
	Column   string
	Kind     reflect.Kind
	Type     reflect.Type
	IsTime   bool
	IsNumber bool
}

// resolveTableName 解析事实/维表名：优先用当前 *gorm.DB 的 NamingStrategy 与 TableName()，
// 禁止假定默认复数表名（本框架 MySQL/SQLite 均为 SingularTable）。
func resolveTableName(db *gorm.DB, model any) (string, error) {
	if model == nil {
		return "", fmt.Errorf("model 不能为空")
	}
	if db != nil {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return "", fmt.Errorf("解析表名失败: %w", err)
		}
		if stmt.Schema != nil && strings.TrimSpace(stmt.Schema.Table) != "" {
			return stmt.Schema.Table, nil
		}
	}
	// 无 DB 时：显式 TableName 接口 > 类型名（不使用复数策略）
	if tn, ok := model.(types.IScopesTableName); ok {
		if name := strings.TrimSpace(tn.TableName()); name != "" {
			return name, nil
		}
	}
	// tabler 接口（gorm 标准）
	if tn, ok := model.(interface{ TableName() string }); ok {
		if name := strings.TrimSpace(tn.TableName()); name != "" {
			return name, nil
		}
	}
	t := derefType(reflect.TypeOf(model))
	if t == nil {
		return "", fmt.Errorf("无法解析 model 类型")
	}
	// 与框架 OLTP 一致：单数蛇形
	namer := schema.NamingStrategy{SingularTable: true}
	return namer.TableName(t.Name()), nil
}

// collectFields 收集 Go 字段与列名。
// 有 db 时用 GORM Schema（列名与当前连接 NamingStrategy 一致）；
// 无 db 时用 SingularTable 命名策略做校验级推断。
func collectFields(model any) (map[string]fieldMeta, error) {
	return collectFieldsWithDB(nil, model)
}

func collectFieldsWithDB(db *gorm.DB, model any) (map[string]fieldMeta, error) {
	if db != nil {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err == nil && stmt.Schema != nil {
			out := make(map[string]fieldMeta, len(stmt.Schema.Fields))
			for _, f := range stmt.Schema.Fields {
				if f == nil || f.DBName == "" {
					continue
				}
				ft := f.FieldType
				if ft.Kind() == reflect.Ptr {
					ft = ft.Elem()
				}
				// 跳过结构体/切片关联（无标量 DataType）
				if f.DataType == "" && ft.Kind() == reflect.Struct && !isTimeType(ft) {
					continue
				}
				meta := fieldMeta{
					GoName:   f.Name,
					Column:   f.DBName,
					Kind:     ft.Kind(),
					Type:     ft,
					IsTime:   isTimeType(ft),
					IsNumber: isNumericType(ft),
				}
				if _, ok := out[f.Name]; !ok {
					out[f.Name] = meta
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		}
	}

	t := derefType(reflect.TypeOf(model))
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("fact/base model 必须是结构体")
	}
	// 与框架 OLTP 默认一致
	namer := schema.NamingStrategy{SingularTable: true}
	out := make(map[string]fieldMeta)
	var walk func(reflect.Type)
	walk = func(tt reflect.Type) {
		tt = derefType(tt)
		if tt == nil || tt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < tt.NumField(); i++ {
			f := tt.Field(i)
			if f.PkgPath != "" && !f.Anonymous {
				continue
			}
			if f.Anonymous {
				walk(f.Type)
				continue
			}
			if f.Tag.Get("gorm") == "-" {
				continue
			}
			ft := derefType(f.Type)
			col := columnNameFromTagOrNamer(f, namer)
			meta := fieldMeta{
				GoName:   f.Name,
				Column:   col,
				Kind:     ft.Kind(),
				Type:     ft,
				IsTime:   isTimeType(ft),
				IsNumber: isNumericType(ft),
			}
			if _, ok := out[f.Name]; !ok {
				out[f.Name] = meta
			}
		}
	}
	walk(t)
	return out, nil
}

func columnNameFromTagOrNamer(f reflect.StructField, namer schema.Namer) string {
	tag := f.Tag.Get("gorm")
	if tag != "" && tag != "-" {
		// column:xxx
		for _, part := range strings.Split(tag, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(strings.ToLower(part), "column:") {
				return strings.TrimSpace(part[len("column:"):])
			}
		}
	}
	return namer.ColumnName("", f.Name)
}

func isTimeType(t reflect.Type) bool {
	if t == nil {
		return false
	}
	return t == reflect.TypeOf(time.Time{}) || t == reflect.TypeOf((*time.Time)(nil)).Elem()
}

func isNumericType(t reflect.Type) bool {
	if t == nil {
		return false
	}
	if t == reflect.TypeOf(decimal.Decimal{}) {
		return true
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func derefType(t reflect.Type) reflect.Type {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func requireField(fields map[string]fieldMeta, name string) (fieldMeta, error) {
	m, ok := fields[name]
	if !ok {
		return fieldMeta{}, fmt.Errorf("字段 %s 不存在", name)
	}
	return m, nil
}
