package nosql

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/local"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/boltdb/bolt"
)

type Bolt struct {
	UserID  uint
	MaxSize int    //库大小限制
	Path    string //库文件路径
	db      *bolt.DB
}

func NewBolt(userid uint) (*Bolt, error) {
	bolt := &Bolt{
		UserID: userid,
	}
	_, err := bolt.getdb()
	return bolt, err
}
func (own *Bolt) GetSize() int64 {
	_, size := local.GetDbFileInfo(own.Path)
	return size
}
func (own *Bolt) ModTime() time.Time {
	t, _ := local.GetDbFileInfo(own.Path)
	return t
}
func (own *Bolt) getdb() (*bolt.DB, error) {
	if own.db == nil {
		if own.Path == "" {
			key := strconv.Itoa(int(own.UserID))
			path, err := local.GetDbPath(key)
			if err != nil {
				return nil, err
			}
			own.Path = path + ".bolt"
		}
		db, err := bolt.Open(own.Path, 0600, &bolt.Options{Timeout: 1 * time.Second})
		if err != nil {
			return nil, err
		}
		defer db.Close()
		own.db = db
	}
	return own.db, nil
}
func (own *Bolt) open() error {
	db, err := bolt.Open(own.Path, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return err
	}
	own.db = db
	return nil
}
func (own *Bolt) Get(id uint, value interface{}) error {
	defer own.Close()
	own.open()
	return own.db.View(func(tx *bolt.Tx) error {
		name := utils.GetTypeName(value)
		b := tx.Bucket([]byte(name))
		if b == nil {
			return nil
		}
		item := b.Get([]byte(strconv.Itoa(int(id))))
		if item != nil && len(item) > 0 {
			return json.Unmarshal(item, value)
		}
		return nil
	})
}

func (own *Bolt) Set(value interface{}) error {
	defer own.Close()
	own.open()
	return own.db.Update(func(tx *bolt.Tx) error {
		name := utils.GetTypeName(value)
		b, err := tx.CreateBucketIfNotExists([]byte(name))
		if err != nil {
			return errors.New("bolt set create bucket error:" + err.Error())
		}
		jbyte, err := json.Marshal(value)
		if err != nil {
			return errors.New(name + "bolt set marshal error:" + err.Error())
		}
		id := utils.GetPropertyValue(value, "ID")
		key := strconv.Itoa(int(id.(uint)))
		return b.Put([]byte(key), jbyte)
	})
}
func (own *Bolt) Delete(value interface{}) error {
	defer own.Close()
	own.open()
	return own.db.Update(func(tx *bolt.Tx) error {
		name := utils.GetTypeName(value)
		b := tx.Bucket([]byte(name))
		if b == nil {
			return nil
		}
		id := utils.GetPropertyValue(value, "ID")
		return b.Delete([]byte(id.(string)))
	})
}
func (own *Bolt) DeleteBucket(value interface{}) error {
	defer own.Close()
	own.open()
	return own.db.Update(func(tx *bolt.Tx) error {
		name := utils.GetTypeName(value)
		return tx.DeleteBucket([]byte(name))
	})
}
func (own *Bolt) AllForEach(fn func(name, key string, value interface{}) error) error {
	defer own.Close()
	own.open()
	return own.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			return b.ForEach(func(k, v []byte) error {
				var value interface{}
				if err := json.Unmarshal(v, &value); err != nil {
					return err
				}
				return fn(string(name), string(k), value)
			})
		})
	})
}
func (own *Bolt) ForEach(model interface{}, fn func(key string, value interface{}) error) error {
	defer own.Close()
	own.open()
	return own.db.View(func(tx *bolt.Tx) error {
		name := utils.GetTypeName(model)
		b := tx.Bucket([]byte(name))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var value interface{}
			if err := json.Unmarshal(v, &value); err != nil {
				return err
			}
			return fn(string(k), value)
		})

	})
}
func (own *Bolt) Close() error {
	return own.db.Close()
}
