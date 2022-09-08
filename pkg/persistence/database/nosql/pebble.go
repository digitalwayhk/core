package nosql

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/local"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/cockroachdb/pebble"
)

type Pebble struct {
	UserID  uint
	MaxSize int    //库大小限制
	Path    string //库文件路径
	db      *pebble.DB
}

func NewPebble(userid uint) (*Pebble, error) {
	pebble := &Pebble{
		UserID: userid,
	}
	_, err := pebble.getdb()
	return pebble, err
}
func (own *Pebble) GetSize() int64 {
	_, size := local.GetDbFileInfo(own.Path)
	return size
}
func (own *Pebble) ModTime() time.Time {
	t, _ := local.GetDbFileInfo(own.Path)
	return t
}
func (own *Pebble) getdb() (*pebble.DB, error) {
	if own.db == nil {
		if own.Path == "" {
			key := strconv.Itoa(int(own.UserID))
			path, err := local.GetDbPath(key)
			if err != nil {
				return nil, err
			}
			own.Path = path + ".pebble"
		}
		db, err := pebble.Open(own.Path, &pebble.Options{})
		if err != nil {
			return nil, err
		}
		defer db.Close()
		own.db = db
	}
	return own.db, nil
}
func (own *Pebble) open() error {
	db, err := pebble.Open(own.Path, &pebble.Options{})
	if err != nil {
		return err
	}
	own.db = db
	return nil
}
func (own *Pebble) Get(id uint, value interface{}) error {
	own.open()
	values, closer, err := own.db.Get([]byte(strconv.Itoa(int(id))))
	if err != nil {
		return err
	}
	if err := closer.Close(); err != nil {
		return err
	}
	if err := own.db.Close(); err != nil {
		return err
	}
	json.Unmarshal(values, value)
	return nil
}

func (own *Pebble) Set(value interface{}) error {
	own.open()
	id := utils.GetPropertyValue(value, "ID")
	key := strconv.Itoa(int(id.(uint)))
	values, err := json.Marshal(value)
	if err != nil {
		return err
	}
	own.db.Set([]byte(key), values, pebble.Sync)
	return own.db.Close()
}
