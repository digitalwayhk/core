package oltp

import (
	"errors"
	"reflect"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TableMaster struct {
	Indexs       map[string]*IndexMaster
	Rows         int
	AvgRowLength int
	DataLength   int
	IndexLength  int
}
type IndexMaster struct {
	Name  string
	Seqno int
	Cid   int
}

func NewTableMaster(db *gorm.DB) *TableMaster {
	tm := &TableMaster{
		Indexs: make(map[string]*IndexMaster),
	}
	//db.Table("index_master").Find(&tm.Indexs)
	return tm
}

func createInBatches(db *gorm.DB, data interface{}) error {
	size := reflect.ValueOf(data).Len()
	if size <= 0 {
		return errors.New("data is empty")
	}
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).CreateInBatches(data, size)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	tx := db.CreateInBatches(data, size)
	if tx.Error != nil {
		logx.Errorf("create in batches error: %v", tx.Error)
		return tx.Error
	}
	return nil
}
func createData(db *gorm.DB, data interface{}) error {
	if db == nil {
		return errors.New("database connection is nil")
	}
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).Create(data)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	if iss, ok := data.(types.ISession); ok {
		db = db.Session(iss.GetSession())
	}
	tx := db.Create(data)
	if tx.Error != nil {
		logx.Errorf("create data error: %v", tx.Error)
		return tx.Error
	}
	return nil
}

func updateData(db *gorm.DB, data interface{}) error {
	if db == nil {
		return errors.New("database connection is nil")
	}
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).Select("*").Updates(data)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	if iss, ok := data.(types.ISession); ok {
		db = db.Session(iss.GetSession())
	}

	// 有 ID 时始终用 ID 定位旧记录，避免业务字段变更后的新 hash 无法匹配旧行。
	// 只有无 ID 模型才回退 hashcode；Select("*") 确保零值字段也被更新。
	if model, ok := data.(types.IModel); ok {
		if model.GetID() > 0 {
			tx := db.Model(data).Where("id = ?", model.GetID()).Select("*").Updates(data)
			if tx.Error != nil {
				logx.Errorf("update data error: %v", tx.Error)
				return tx.Error
			}
			return nil
		}

		hashcode := getHashcode(data)
		if hashcode != "" {
			tx := db.Model(data).Where("hashcode = ?", hashcode).Select("*").Updates(data)
			if tx.Error != nil {
				logx.Errorf("update data error: %v", tx.Error)
				return tx.Error
			}
			return nil
		}

		tx := db.Model(data).Where("id = ?", model.GetID()).Select("*").Updates(data)
		if tx.Error != nil {
			logx.Errorf("update data error: %v", tx.Error)
			return tx.Error
		}
		return nil
	}

	// Save 本身更新全部字段（含零值），保留作为兜底
	tx := db.Save(data)
	if tx.Error != nil {
		logx.Errorf("update data error: %v", tx.Error)
		return tx.Error
	}
	return nil
}

// getHashcode 通过反射获取 hashcode 字段
func getHashcode(data interface{}) string {
	if rowcode, ok := data.(types.IRowCode); ok {
		return rowcode.GetHash()
	}
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return ""
	}

	field := v.FieldByName("Hashcode")
	if !field.IsValid() {
		return ""
	}

	if field.Kind() == reflect.String {
		return field.String()
	}

	return ""
}

func deleteData(db *gorm.DB, data interface{}) error {
	if db == nil {
		return errors.New("database connection is nil")
	}
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).Delete(data)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	if iss, ok := data.(types.ISession); ok {
		db = db.Session(iss.GetSession())
	}
	if rowcode, ok := data.(types.IRowCode); ok {
		code := rowcode.GetHash()
		if code != "" {
			tx := db.Where("hashcode = ?", code).Delete(data)
			if tx.Error != nil {
				logx.Errorf("delete data error: %v", tx.Error)
				return tx.Error
			}
			return nil
		}
	}
	if model, ok := data.(types.IModel); ok {
		if model.GetID() > 0 {
			tx := db.Where("id = ?", model.GetID()).Delete(data)
			if tx.Error != nil {
				logx.Errorf("delete data error: %v", tx.Error)
				return tx.Error
			}
			return nil
		}
	}
	tx := db.Delete(data)
	if tx.Error != nil {
		logx.Errorf("delete data error: %v", tx.Error)
		return tx.Error
	}
	return nil
}
func sqlload(dbsql types.IDBSQL, db *gorm.DB, item *types.SearchItem, result interface{}) error {
	sql := dbsql.SearchSQL()
	if sql == "" {
		return errors.New("sql is empty")
	}
	query, args := item.Where(db)
	source := func() *gorm.DB { return db.Model(item.Model).Table("(?) AS a", db.Raw(sql)).Where(query, args...) }
	item.Total = 0
	if !item.SkipCount {
		if err := source().Count(&item.Total).Error; err != nil {
			return err
		}
	}
	if item.SkipCount || item.Total > 0 {
		query := source().Scopes(paginate(item.Page, item.Size))
		if len(item.SortList) > 0 {
			query = query.Order(item.Order())
		}
		return query.Find(result).Error
	}
	return nil
}

func load(db *gorm.DB, item *types.SearchItem, result interface{}) error {
	if item.Context != nil {
		db = db.WithContext(item.Context)
	}
	if item.SkipCount {
		item.Total = 0
	}
	if dbsql, ok := item.Model.(types.IDBSQL); ok {
		return sqlload(dbsql, db, item, result)
	}
	if ist, ok := item.Model.(types.IScopes); ok {
		if item.SkipCount {
			sdb := ist.ScopesHandler()(db)
			query, args := item.Where(sdb)
			return sdb.Model(item.Model).Where(query, args...).Scopes(paginate(item.Page, item.Size)).Order(item.Order()).Find(result).Error
		}
		tx := db.Scopes(func(d *gorm.DB) *gorm.DB {
			sh := ist.ScopesHandler()
			sdb := sh(d)
			query, args := item.Where(sdb)
			return sdb.Where(query, args...)
		}).Model(item.Model).Count(&item.Total)
		if tx.Error != nil {
			logx.Errorw("database_query_failed",
				logx.Field("operation", "scoped_count"),
				logx.Field("error", tx.Error),
			)
			return tx.Error
		}
		if item.Total > 0 {
			tx = db.Scopes(func(d *gorm.DB) *gorm.DB {
				sh := ist.ScopesHandler()
				sdb := sh(d)
				page := paginate(item.Page, item.Size)
				sdb = page(sdb)
				query, args := item.Where(sdb)
				return sdb.Where(query, args...)
			}).Model(item.Model).Order(item.Order()).Find(result)
			if tx.Error != nil {
				logx.Errorw("database_query_failed",
					logx.Field("operation", "scoped_find"),
					logx.Field("error", tx.Error),
				)
				return tx.Error
			}
		}
		return nil
	}
	query, args := item.Where(db)
	if item.SkipCount {
		db = db.Model(item.Model).Scopes(paginate(item.Page, item.Size))
		if len(item.WhereList) > 0 {
			db = db.Where(query, args...)
		}
		if item.Order() != "" {
			db = db.Order(item.Order())
		}
		if item.IsPreload {
			db = db.Preload(clause.Associations)
		}
		return db.Scan(result).Error
	}
	tx := db.Model(item.Model).Where(query, args...).Count(&item.Total)
	if tx.Error != nil {
		return tx.Error
	}
	if item.Total > 0 {
		db = db.Model(item.Model).Scopes(paginate(item.Page, item.Size))
		if len(item.WhereList) > 0 {
			db = db.Where(query, args...)
		}
		if item.Order() != "" {
			db = db.Order(item.Order())
		}
		if item.IsPreload {
			db = db.Preload(clause.Associations)
		}
		tx = db.Scan(result)
		if tx.Error != nil {
			logx.Errorf("load find error: %v", tx.Error)
			return tx.Error
		}
	}
	return nil
}
func sum(db *gorm.DB, item *types.SearchItem, result interface{}) error {
	if item.Context != nil {
		db = db.WithContext(item.Context)
	}
	var fdb *gorm.DB
	if ist, ok := item.Model.(types.IScopes); ok {
		fdb = db.Scopes(func(d *gorm.DB) *gorm.DB {
			sh := ist.ScopesHandler()
			sdb := sh(d)
			return sdb
		}).Model(item.Model)
	} else {
		fdb = db.Model(item.Model)
	}
	query, args := item.Where(fdb)
	group := item.Group()
	scal := "sum"
	if item.Statistical != "" {
		scal = item.Statistical
	}
	an := scal + item.StatField
	pnll := scal + "(" + item.GetColumnName(item.StatField) + ") " + fdb.Statement.Quote(an)
	if strings.ToLower(scal) == "count" {
		pnll = "count(1) as total"
	}
	if group != "" {
		pnll = group + "," + pnll
		fdb = fdb.Group(group)
	}
	tx := fdb.Where(query, args...).Select(pnll).Scan(result)
	return tx.Error
}
func paginate(page, size int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page == 0 {
			page = 1
		}
		switch {
		case size > 5000:
			size = 5000
		case size <= 0:
			size = 10
		}
		offset := (page - 1) * size
		return db.Offset(offset).Limit(size)
	}
}
func DropTable(db *gorm.DB, dst interface{}) error {
	err := db.Migrator().DropTable(dst)
	if err != nil {
		return err
	}
	return nil
}

// existsData 判断数据行是否存在（通用方法）
func existsData(db *gorm.DB, data interface{}) (bool, error) {
	if db == nil {
		return false, errors.New("database connection is nil")
	}

	// 🔧 优先使用 hashcode 作为查询条件
	if rowcode, ok := data.(types.IRowCode); ok {
		hashcode := rowcode.GetHash()
		if hashcode == "" {
			hashcode = rowcode.GetHash()
		}

		if hashcode != "" {
			var count int64
			err := db.Model(data).Where("hashcode = ?", hashcode).Count(&count).Error
			if err != nil {
				return false, err
			}
			return count > 0, nil
		}
	}

	// 🔧 使用 ID 作为查询条件
	if model, ok := data.(types.IModel); ok {
		id := model.GetID()
		if id > 0 {
			var count int64
			err := db.Model(data).Where("id = ?", id).Count(&count).Error
			if err != nil {
				return false, err
			}
			return count > 0, nil
		}
	}

	// 🔧 通过反射获取主键字段
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return false, errors.New("data must be a struct or pointer to struct")
	}

	// 尝试获取 ID 字段
	if idField := v.FieldByName("ID"); idField.IsValid() && idField.Kind() == reflect.Int64 {
		id := idField.Int()
		if id > 0 {
			var count int64
			err := db.Model(data).Where("id = ?", id).Count(&count).Error
			if err != nil {
				return false, err
			}
			return count > 0, nil
		}
	}

	return false, errors.New("cannot determine primary key: no hashcode, ID field, or IModel interface found")
}

// existsByCondition 根据自定义条件判断数据行是否存在（通用方法）
func existsByCondition(db *gorm.DB, model interface{}, condition string, args ...interface{}) (bool, error) {
	if db == nil {
		return false, errors.New("database connection is nil")
	}

	if condition == "" {
		return false, errors.New("condition cannot be empty")
	}

	var count int64
	err := db.Model(model).Where(condition, args...).Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// existsByHashcode 根据 hashcode 判断数据行是否存在（通用方法）
func existsByHashcode(db *gorm.DB, model interface{}, hashcode string) (bool, error) {
	if db == nil {
		return false, errors.New("database connection is nil")
	}

	if hashcode == "" {
		return false, errors.New("hashcode cannot be empty")
	}

	var count int64
	err := db.Model(model).Where("hashcode = ?", hashcode).Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// existsByID 根据 ID 判断数据行是否存在（通用方法）
func existsByID(db *gorm.DB, model interface{}, id int64) (bool, error) {
	if db == nil {
		return false, errors.New("database connection is nil")
	}

	if id <= 0 {
		return false, errors.New("id must be greater than 0")
	}

	var count int64
	err := db.Model(model).Where("id = ?", id).Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
