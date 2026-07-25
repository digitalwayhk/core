package adapter

import (
	"errors"
	"fmt"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

var ErrCacheAdapterUnavailable = errors.New("cache adapter is unavailable")

type CacheAdapter struct {
	DbName string
}

func (own *CacheAdapter) Get(key string) (interface{}, error) {
	db, err := own.getCacheDB()
	if err != nil {
		return nil, err
	}
	return db.Get(key)
}

func (own *CacheAdapter) Set(key string, value interface{}, seconds int) error {
	db, err := own.getCacheDB()
	if err != nil {
		return err
	}
	return db.Set(key, value, seconds)
}

func (own *CacheAdapter) Del(key string) error {
	db, err := own.getCacheDB()
	if err != nil {
		return err
	}
	return db.Del(key)
}

func (own *CacheAdapter) Scan() ([]string, error) {
	db, err := own.getCacheDB()
	if err != nil {
		return nil, err
	}
	return db.Scan()
}

func (own *CacheAdapter) Search(prefix string) ([]string, error) {
	db, err := own.getCacheDB()
	if err != nil {
		return nil, err
	}
	return db.Search(prefix)
}

func (own *CacheAdapter) getCacheDB() (types.ICache, error) {
	return nil, fmt.Errorf("%w: database %q has no configured provider", ErrCacheAdapterUnavailable, own.DbName)
}
