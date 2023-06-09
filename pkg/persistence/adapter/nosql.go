package adapter

import (
	"log"

	"github.com/digitalwayhk/core/pkg/persistence/models"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"gorm.io/gorm"
)

type NosqlAdapter struct {
	isTransaction bool //是否开启事务
}

func NewNosqlAdapter() *NosqlAdapter {
	return &NosqlAdapter{
		isTransaction: false,
	}
}
func (own *NosqlAdapter) DBRaw(sql string, values ...interface{}) (tx *gorm.DB) {

	return nil
}

//// ReadType 動態轉化 Symbol 為 bson 所需字段
//var ReadType = map[string]string{
//	">":  "$gt",
//	"<":  "$lt",
//	">=": "$gte",
//	"=<": "$lte",
//	"!=": "$ne",
//}

func (own NosqlAdapter) Transaction() {
	log.Println("implement Mongo Transaction")
}

func (own NosqlAdapter) SetSaveType(saveType types.SaveType) {
	//TODO implement me
}

func (own NosqlAdapter) Load(item *types.SearchItem, result interface{}) error {
	//TODO implement me
	db, err := own.getNosqlDB(item.Model)
	if err != nil {
		return err
	}
	err = db.Load(item, result)
	if err != nil {
		return err
	}
	return nil
}
func (own *NosqlAdapter) Raw(sql string, data interface{}) error {

	return nil
}

func (own NosqlAdapter) Insert(data interface{}) error {
	//TODO implement me
	db, err := own.getNosqlDB(data)
	if err != nil {
		return err
	}
	err = db.Insert(data)
	if err != nil {
		return err
	}
	return nil
}

func (own NosqlAdapter) Update(data interface{}) error {
	//TODO implement me
	db, err := own.getNosqlDB(data)
	if err != nil {
		return err
	}
	err = db.Update(data)
	if err != nil {
		return err
	}
	return nil
}

func (own NosqlAdapter) Delete(data interface{}) error {
	//TODO implement me
	db, err := own.getNosqlDB(data)
	if err != nil {
		return err
	}
	err = db.Delete(data)
	if err != nil {
		return err
	}
	return nil
}

func (own NosqlAdapter) Commit() error {
	//TODO implement me
	log.Println("implement Mongo commit")
	return nil
}

func (own NosqlAdapter) Back() error {
	//TODO implement me
	log.Println("implement Mongo back")
	return nil
}

func (own *NosqlAdapter) getNosqlDB(model interface{}) (types.IDataBase, error) {
	idbn, err := getIDBName(model)
	if err != nil {
		return nil, err
	}
	name := idbn.GetRemoteDBName()
	idb, err := models.GetConfigRemoteDB(name, 0, false)
	if err != nil {
		return nil, err
	}
	return idb, nil
}
func (own *NosqlAdapter) GetRunDB() interface{} {
	return nil
}

//func loadMongo(db *mongo.Database, item *types.SearchItem, result interface{}) error {
//
//	var limit int64 = 10 // 默認只獲取 10 條記錄
//	if item.Total != 0 {
//		limit = item.Total
//	}
//	findOptions := options.Find().SetLimit(limit)
//
//	if len(item.SortList) > 0 {
//		var sort bson.D
//		for _, data := range item.SortList {
//			order := 1
//			if data.IsDesc {
//				order = -1
//			}
//			sort = append(sort, bson.E{Key: data.Column, Value: order})
//		}
//		findOptions.SetSort(sort)
//	}
//
//	var filter bson.D // i.e. {"userId", 12345}, {"terminal", 1}
//	for _, data := range item.WhereList {
//		if data.Symbol != "" {
//			filter = append(filter, bson.E{Key: data.Column, Value: bson.M{ReadType[data.Symbol]: data.Value}})
//		} else {
//			filter = append(filter, bson.E{Key: data.Column, Value: data.Value})
//		}
//	}
//	collection := db.Collection(getModelName(item.Model))
//	cursor, err := collection.Find(context.TODO(), filter, findOptions)
//	if err != nil {
//		return err
//	}
//
//	if err = cursor.All(context.TODO(), result); err != nil {
//		log.Println("Mongo decode err: ", err)
//	}
//	return nil
//}
//
//func insertMongoData(db *mongo.Database, data interface{}) error {
//
//	collection := db.Collection(getModelName(data))
//
//	if _, err := collection.InsertOne(context.TODO(), data); err != nil {
//		return err
//	}
//	return nil
//}
//
//func updateMongoData(db *mongo.Database, data interface{}) error {
//	collection := db.Collection(getModelName(data))
//
//	// TODO: Filter setup
//	val := reflect.ValueOf(data).Elem()
//	id := val.FieldByName("ID").Interface()
//	if _, err := collection.ReplaceOne(context.TODO(), bson.D{{"_id", id}}, data); err != nil {
//		return err
//	}
//
//	return nil
//}
//
//func deleteMongoData(db *mongo.Database, data interface{}) error {
//	collection := db.Collection(getModelName(data))
//
//	val := reflect.ValueOf(data).Elem()
//	id := val.FieldByName("ID").Interface()
//	if _, err := collection.DeleteOne(context.TODO(), bson.D{{"_id", id}}); err != nil {
//		return err
//	}
//	return nil
//}
//
//func getModelName(data interface{}) string {
//	modelName := reflect.TypeOf(data).String() // 例如 *users.LoginRecord
//	return strings.Split(modelName, ".")[1]
//}
