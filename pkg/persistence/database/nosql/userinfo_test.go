package nosql

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

type UserInformation struct {
	*entity.Model
	UserID         string     `json:"userId"`
	Owner          string     `json:"owner"`
	Name           string     `json:"name"`
	CreatedTime    string     `json:"createdTime"`
	Organization   string     `json:"organization"`
	ClientIp       string     `json:"clientIp"`
	User           string     `json:"user"`
	Method         string     `json:"method"`
	RequestUri     string     `json:"requestUri"`
	Action         string     `json:"action"`
	IsTriggered    bool       `json:"isTriggered"`
	FirstLoginTime *time.Time `json:"firstLoginTime"`
	LastLoginTime  *time.Time `json:"lastLoginTime"`
	LastActiveTime *time.Time `json:"lastActiveTime"`
	LastDeviceID   string     `json:"lastDeviceId"`
}

func NewUserInformation() *UserInformation {
	return &UserInformation{
		Model: entity.NewModel(),
	}
}
func (own *UserInformation) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
func (own *UserInformation) GetHash() string {
	return own.UserID
}
func (own *UserInformation) GetLocalDBName() string {
	return "test_funds"
}
func (own *UserInformation) GetRemoteDBName() string {
	return "test_funds_remote"
}

type Us_ModelList[T types.IModel] struct {
	*entity.ModelList[T]
	badgerDB *PrefixedBadgerDB[T]
}

func newInfoTestDB[T types.IModel](t *testing.T) *Us_ModelList[T] {
	t.Helper()
	name := reflect.TypeOf((*T)(nil)).Elem().Name()
	if list, ok := userTypeList.Load(name); ok {
		return list.(*Us_ModelList[T])
	}
	// ✅ 加锁
	initLock.Lock()
	defer initLock.Unlock()
	// ✅ 第二次检查（避免并发重复创建）
	if list, ok := userTypeList.Load(name); ok {
		return list.(*Us_ModelList[T])
	}
	db, err := NewSharedBadgerDB[T](t.TempDir())
	if err != nil {
		t.Fatalf("NewSharedBadgerDB 失败: %v", err)
	}
	cfg := &oltp.Config{
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "root",
		Password: "your_password",
	}
	action := oltp.NewMySQL(cfg)
	list := &Us_ModelList[T]{
		ModelList: entity.NewModelList[T](action),
		badgerDB:  db,
	}
	db.SetSyncDB(list.ModelList)
	t.Cleanup(func() { db.Close() })
	userTypeList.Store(name, list)
	return list
}

var userTypeList = sync.Map{}
var initLock sync.Mutex // ✅ 添加全局锁
func TestUserInformation(t *testing.T) {
	infoList := newInfoTestDB[UserInformation](t)
	info := NewUserInformation()
	info.UserID = "1012"
	info.Name = "string"
	info.ClientIp = "127.0.0.1"
	info.Method = "string"
	info.RequestUri = "/api/test"
	info.Action = "testAction"
	info.IsTriggered = true

	err := infoList.badgerDB.Set(info, 0)
	if err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	fund := newFund("1009", "string", 100.0)
	fundList := newInfoTestDB[testFund](t)
	err = fundList.badgerDB.Set(fund, 0)
	if err != nil {
		t.Fatalf("Set fund 失败: %v", err)
	}

	// 轮询等待后台同步写入 MySQL（验证自动同步可靠性，超时 5s）
	var rows []*UserInformation
	var total int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, total, err = infoList.SearchAll(1, 10)
		if err == nil && total >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("SearchAll 失败: %v", err)
	}
	if total != 1 {
		t.Fatalf("预期 total=1, 实际 %d（同步超时）", total)
	}
	retrieved := rows[0]
	if retrieved.UserID != info.UserID || retrieved.Name != info.Name || retrieved.ClientIp != info.ClientIp {
		t.Fatalf("数据不匹配: 预期 %+v, 实际 %+v", info, retrieved)
	}

	var rows1 []*testFund
	var total1 int64
	var err1 error
	deadline1 := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline1) {
		rows1, total1, err1 = fundList.SearchAll(1, 10)
		if err1 == nil && total1 >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err1 != nil {
		t.Fatalf("SearchAll fund 失败: %v", err1)
	}
	if total1 != 1 {
		t.Fatalf("预期 fund total=1, 实际 %d（同步超时）", total1)
	}
	retrievedFund := rows1[0]
	// 不比较 ID：MySQL 会自增分配 ID，本地新建时 ID=0，必然不等
	if retrievedFund.UserID != fund.UserID || retrievedFund.Market != fund.Market || retrievedFund.Balance != fund.Balance {
		t.Fatalf("fund 数据不匹配: 预期 %+v, 实际 %+v", fund, retrievedFund)
	}
}
