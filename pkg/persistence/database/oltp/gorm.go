package oltp

import (
	"errors"
	"reflect"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/types"

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
		return tx.Error
	}
	return nil
}
func createData(db *gorm.DB, data interface{}) error {
	// t, v := utils.GetTypeAndValue(data)
	// if t.Kind() == reflect.Array || t.Kind() == reflect.Slice {
	// 	size := v.Len()
	// 	var err error
	// 	if size > 0 {
	// 		err = createInBatches(db, data)
	// 	}
	// 	return err
	// }
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).Create(data)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	tx := db.Create(data)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}
func updateData(db *gorm.DB, data interface{}) error {
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).Updates(data)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	tx := db.Save(data)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}
func deleteData(db *gorm.DB, data interface{}) error {
	if ist, ok := data.(types.IScopes); ok {
		tx := db.Scopes(ist.ScopesHandler()).Delete(data)
		if tx.Error != nil {
			return tx.Error
		}
		return nil
	}
	tx := db.Delete(data)
	if tx.Error != nil {
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
	name := ""
	swhere := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		tx = tx.Model(item.Model)
		tx = tx.Where(query, args...)
		tx = tx.Find(result)
		name = tx.Statement.Schema.Table
		return tx
	})
	if name != "" {
		name = "`" + name + "`"
	}
	sqlwhere := strings.Replace(swhere, name, "("+sql+") a", 1)
	tx := db.Raw("select count(*) from (" + sqlwhere + ") a").Count(&item.Total)
	if tx.Error != nil {
		return tx.Error
	}
	if item.Total > 0 {
		swhere := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			tx = tx.Model(item.Model)
			tx = tx.Where(query, args...)
			tx = tx.Order(item.Order())
			if item.Total > int64(item.Size) {
				tx = tx.Scopes(paginate(item.Page, item.Size))
			}
			tx = tx.Find(result)
			return tx
		})
		sqlwhere := strings.Replace(swhere, name, "("+sql+") a", 1)
		tx = db.Raw(sqlwhere).Find(result)
		if tx.Error != nil {
			return tx.Error
		}
	}
	return nil
}
func load(db *gorm.DB, item *types.SearchItem, result interface{}) error {
	if dbsql, ok := item.Model.(types.IDBSQL); ok {
		return sqlload(dbsql, db, item, result)
	}
	if ist, ok := item.Model.(types.IScopes); ok {
		tx := db.Scopes(func(d *gorm.DB) *gorm.DB {
			sh := ist.ScopesHandler()
			sdb := sh(d)
			query, args := item.Where(sdb)
			return sdb.Where(query, args...)
		}).Model(item.Model).Count(&item.Total)
		if tx.Error != nil {
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
				return tx.Error
			}
		}
		return nil
	}
	query, args := item.Where(db)
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
		tx = db.Find(result)
		if tx.Error != nil {
			return tx.Error
		}
	}
	return nil
}
func sum(db *gorm.DB, item *types.SearchItem, result interface{}) error {
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
	if group != "" {
		scal = group + "," + scal
		fdb = fdb.Group(group)
	}
	an := scal + item.StatField
	pnll := scal + "(" + item.StatField + ") " + an
	if strings.ToLower(scal) == "count" {
		pnll = "count(1) as total"
	}
	tx := fdb.Where(query, args...).Select(pnll).First(result)
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
