package test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type Conn struct {
	Addr  string
	State int
}
type User struct {
	done uint32
	Id   int64
	mu   sync.Mutex
	c    *Conn
}

var i = 0

func newConn() (*Conn, error) {
	fmt.Println("newConn")
	div := i
	i++
	if div == 0 {
		return nil, errors.New("the divisor is zero")
	}
	k := 1 / div
	return &Conn{"127.0.0.1:8080", k}, nil
}
func mustNewConn() *Conn {
	conn, err := newConn()
	if err != nil {
		panic(err)
	}
	return conn
}
func getInstance(user *User) *Conn {
	if atomic.LoadUint32(&user.done) == 0 {
		user.mu.Lock()
		defer user.mu.Unlock()

		if user.done == 0 {
			defer func() {
				if r := recover(); r == nil {
					defer atomic.StoreUint32(&user.done, 1)
				}
			}()

			user.c = mustNewConn()
		}
	}
	return user.c
}

type Test1 struct {
	Name string
	Age  int
	Data interface{}
}

func Test_map_struct(t *testing.T) {
	ts := make(map[interface{}]string)
	t1 := &Test1{Name: "1", Age: 2}
	ts[t1] = "test1"
	ts[Test1{Name: "2"}] = "2"
	ts[Test1{Name: "3"}] = "3"

	
	t.Log(ts[t1])
}
func test(key interface{}, mp map[interface{}]string) {
	fmt.Println(mp[key])
}
