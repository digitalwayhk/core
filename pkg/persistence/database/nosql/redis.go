package nosql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisUrl = "%s:%d"

type Redis struct {
	Host string
	Port uint
	Pass string
	Name string
}

func (own *Redis) Get(key string) (interface{}, error) {
	db, err := own.GetRedis()
	if err != nil {
		return nil, err
	}
	return db.Get(context.Background(), key).Result()
}

func (own *Redis) Set(key string, value interface{}, seconds int) error {
	db, err := own.GetRedis()
	if err != nil {
		return err
	}
	// Go-Redis default setting: seconds == 0 -> Not expire
	return db.Set(context.Background(), key, value, time.Duration(seconds)*time.Second).Err()
}

func (own *Redis) Del(key string) error {
	db, err := own.GetRedis()
	if err != nil {
		return err
	}
	return db.Del(context.Background(), key).Err()
}

func (own *Redis) Scan() ([]string, error) {
	var keys []string

	db, err := own.GetRedis()
	if err != nil {
		return nil, err
	}

	iter := db.Scan(context.Background(), 0, "*", 0).Iterator()
	for iter.Next(context.TODO()) {
		keys = append(keys, iter.Val())
	}
	return keys, nil
}

func (own *Redis) Search(prefix string) ([]string, error) {
	var keys []string

	db, err := own.GetRedis()
	if err != nil {
		return nil, err
	}

	iter := db.Scan(context.Background(), 0, prefix+"*", 0).Iterator()
	for iter.Next(context.TODO()) {
		keys = append(keys, iter.Val())
	}
	return keys, nil
}

func NewRedis(host, pass string, port uint) *Redis {
	return &Redis{
		Host: host,
		Port: port,
		Pass: pass,
	}
}

func (own *Redis) GetRedis() (*redis.Client, error) {

	addr := fmt.Sprintf(redisUrl, own.Host, own.Port)

	redisCli := redis.NewClient(&redis.Options{
		Addr:     addr,                    // host:port of the redis server
		Password: own.Pass,                // no password set
		DB:       GetRedisDBInt(own.Name), // use default DB -> 0
	})
	if err := redisCli.Ping(context.TODO()).Err(); err != nil {
		return nil, err
	}

	return redisCli, nil
}

func GetRedisDBInt(dbName string) int {

	name := strings.ToUpper(dbName)
	switch name {
	case "TRANS":
		return 1
	case "TICKER":
		return 2
	case "PUSH":
		return 3
	default:
		return 0 // MAINS
	}
}
