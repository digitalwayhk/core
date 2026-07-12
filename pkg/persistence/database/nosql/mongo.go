package nosql

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ReadType 動態轉化 Symbol 為 bson 所需字段
var ReadType = map[string]string{
	">":  "$gt",
	"<":  "$lt",
	">=": "$gte",
	"=<": "$lte",
	"!=": "$ne",
}

var mongoUri = "mongodb://%s:%s@%s:%d"

var (
	ErrMongoTransactionsUnsupported = errors.New("mongo transactions are unsupported")
	ErrMongoRawUnsupported          = errors.New("mongo raw operations are unsupported")
	ErrMongoStatisticsUnsupported   = errors.New("mongo statistical queries are unsupported")
)

type Mongo struct {
	Name    string
	Host    string
	Port    uint
	User    string
	Pass    string
	db      *mongo.Database
	TimeOut time.Duration
}

func (own *Mongo) init(data interface{}) error {
	if own.Name == "" {
		err := own.GetDBName(data)
		if err != nil {
			return err
		}
	}
	if own.db == nil {
		_, err := own.GetMongo()
		if err != nil {
			return err
		}
	}
	return nil
}

func (own *Mongo) Transaction() error {
	return ErrMongoTransactionsUnsupported
}

func (own *Mongo) Load(item *types.SearchItem, result interface{}) error {
	err := own.init(item.Model)
	if err != nil {
		return err
	}
	if item.IsStatistical {
		return sum(own.db, item, result)
	}
	return loadMongo(own.db, item, result)
}
func (own *Mongo) Raw(sql string, data interface{}) error {
	return ErrMongoRawUnsupported
}
func (own *Mongo) Exec(sql string, data interface{}) error {
	return ErrMongoRawUnsupported
}
func (own *Mongo) GetModelDB(model interface{}) (interface{}, error) {
	err := own.init(model)
	return own.db, err
}
func (own *Mongo) Insert(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	return insertMongoData(own.db, data)
}

func (own *Mongo) Update(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	return updateMongoData(own.db, data)
}

func (own *Mongo) Delete(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	return deleteMongoData(own.db, data)
}

func (own *Mongo) Commit() error {
	return ErrMongoTransactionsUnsupported
}

func (own *Mongo) Rollback() error {
	return ErrMongoTransactionsUnsupported
}

func (own *Mongo) GetDBName(data interface{}) error {
	if idb, ok := data.(types.IDBName); ok {
		own.Name = idb.GetRemoteDBName()
		if own.Name == "" {
			return errors.New("db name is empty")
		}
		return nil
	}
	return errors.New("db name is empty")
}

func (own *Mongo) HasTable(model interface{}) error {
	// MongoDB collections are schema-less and created on first write.
	return nil
}

func NewMongo(host, user, pass string, port uint) *Mongo {
	return &Mongo{
		Host:    host,
		Port:    port,
		User:    user,
		Pass:    pass,
		TimeOut: 10 * time.Second,
	}
}
func (own *Mongo) GetRunDB() interface{} {
	return own.db
}
func (own *Mongo) GetMongo() (*mongo.Database, error) {
	if own.db == nil {
		dsn := fmt.Sprintf(mongoUri, own.User, own.Pass, own.Host, own.Port)
		timeout := own.TimeOut
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
		if err != nil {
			return nil, err
		}
		if err = client.Ping(ctx, nil); err != nil {
			_ = client.Disconnect(context.Background())
			return nil, fmt.Errorf("ping mongo: %w", err)
		}
		own.db = client.Database(own.Name)
	}
	return own.db, nil
}

func getModelName(data interface{}) string {
	modelName := reflect.TypeOf(data).String() // 例如 *users.LoginRecord
	return strings.Split(modelName, ".")[1]
}

func insertMongoData(db *mongo.Database, data interface{}) error {

	collection := db.Collection(getModelName(data))

	if _, err := collection.InsertOne(context.TODO(), data); err != nil {
		return err
	}
	return nil
}

func sum(db *mongo.Database, item *types.SearchItem, result interface{}) error {
	return ErrMongoStatisticsUnsupported
}

func loadMongo(db *mongo.Database, item *types.SearchItem, result interface{}) error {

	var limit int64 = 10 // 默認只獲取 10 條記錄
	if item.Total != 0 {
		limit = item.Total
	}
	findOptions := options.Find().SetLimit(limit)

	if len(item.SortList) > 0 {
		var sort bson.D
		for _, data := range item.SortList {
			order := 1
			if data.IsDesc {
				order = -1
			}
			sort = append(sort, bson.E{Key: data.Column, Value: order})
		}
		findOptions.SetSort(sort)
	}

	var filter bson.D // i.e. {"userId", 12345}, {"terminal", 1}
	for _, data := range item.WhereList {
		if data.Symbol != "" {
			filter = append(filter, bson.E{Key: data.Column, Value: bson.M{ReadType[data.Symbol]: data.Value}})
		} else {
			filter = append(filter, bson.E{Key: data.Column, Value: data.Value})
		}
	}
	collection := db.Collection(getModelName(item.Model))
	cursor, err := collection.Find(context.TODO(), filter.Map(), findOptions)
	if err != nil {
		return err
	}

	return cursor.All(context.TODO(), result)
}

func updateMongoData(db *mongo.Database, data interface{}) error {
	collection := db.Collection(getModelName(data))

	// TODO: Filter setup
	val := reflect.ValueOf(data).Elem()
	id := val.FieldByName("ID").Interface()
	if _, err := collection.ReplaceOne(context.TODO(), bson.D{{Key: "_id", Value: id}}, data); err != nil {
		return err
	}

	return nil
}

func deleteMongoData(db *mongo.Database, data interface{}) error {
	collection := db.Collection(getModelName(data))

	val := reflect.ValueOf(data).Elem()
	id := val.FieldByName("ID").Interface()
	if _, err := collection.DeleteOne(context.TODO(), bson.D{{Key: "_id", Value: id}}); err != nil {
		return err
	}
	return nil
}
