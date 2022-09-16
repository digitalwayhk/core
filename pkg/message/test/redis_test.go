package test

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/adapter"
	pkg "github.com/digitalwayhk/core/pkg/persistence/models"
	"strconv"
	"testing"
)

func init() {

	pkg.GetRemoteDBHandler = func(dbconfig *pkg.RemoteDbConfig) {
		if dbconfig.Host == "" {
			dbconfig.Host = "127.0.0.1"
			dbconfig.Port = 6379
			dbconfig.Pass = ""
			dbconfig.Name = "mains"
			dbconfig.ConnectType = 0
			dbconfig.DataBaseType = "redis"
		}
	}
}

func Test_SetRedis(t *testing.T) {
	redis := &adapter.CacheAdapter{DbName: "mains"}

	err := redis.Set("language", "golang", 10*60)
	if err != nil {
		fmt.Println("Redis set failed: ", err)
	}
}

func Test_GetRedis(t *testing.T) {
	redis := &adapter.CacheAdapter{DbName: "mains"}
	val, err := redis.Get("language")
	if err != nil {
		fmt.Println("Redis get failed")
	}
	fmt.Println("value: ", val)
}

func Test_DelRedis(t *testing.T) {
	redis := &adapter.CacheAdapter{DbName: "mains"}
	err := redis.Del("language")
	if err != nil {
		fmt.Println("Redis del failed")
	}
	fmt.Println("Redis del done")
}

func Test_MultiSetRedis(t *testing.T) {
	redis := &adapter.CacheAdapter{DbName: "mains"}
	for i := 0; i < 5000; i++ {
		err := redis.Set("key_f_"+strconv.Itoa(i), "value_"+strconv.Itoa(i), 60*60*5)
		if err != nil {
			fmt.Println("Redis set failed")
		}
	}
}

func Test_ScanRedis(t *testing.T) {
	redis := &adapter.CacheAdapter{DbName: "mains"}
	keys, err := redis.Scan()
	if err != nil {
		fmt.Println("Redis scan failed")
	}
	fmt.Println("Redis Key num: ", len(keys))
	//for _, key := range keys {
	//	fmt.Printf("ScanRedis: %v\n", key)
	//}
}

func Test_SearchRedis(t *testing.T) {
	redis := &adapter.CacheAdapter{DbName: "mains"}
	keys, err := redis.Search("key_a")
	if err != nil {
		fmt.Println("Redis scan failed")
	}
	fmt.Println("Redis Key num: ", len(keys))
	//for _, key := range keys {
	//	fmt.Printf("ScanRedis: %v\n", key)
	//}
}
