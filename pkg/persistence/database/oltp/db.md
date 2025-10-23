// 使用ATTACH方式的跨库事务
func ExampleAttachTransaction() error {
    cdt := NewCrossDBTransaction("main.ldb")
    
    // 附加其他数据库
    if err := cdt.AttachDB("users", "users.ldb"); err != nil {
        return err
    }
    if err := cdt.AttachDB("orders", "orders.ldb"); err != nil {
        return err
    }
    
    // 开始事务
    if err := cdt.Begin(); err != nil {
        return err
    }
    
    // 在事务中操作多个数据库
    // 注意：需要使用别名前缀访问表
    err := cdt.tx.Exec("INSERT INTO users.user_info (name) VALUES (?)", "John").Error
    if err != nil {
        cdt.Rollback()
        return err
    }
    
    err = cdt.tx.Exec("INSERT INTO orders.order_info (user_id, amount) VALUES (?, ?)", 1, 100.0).Error
    if err != nil {
        cdt.Rollback()
        return err
    }
    
    return cdt.Commit()
}

// 使用多DB管理器的分布式事务模拟
func ExampleMultiDBTransaction() error {
    mdt := NewMultiDBTransaction()
    
    // 添加多个数据库
    if err := mdt.AddDatabase("users", "users.ldb"); err != nil {
        return err
    }
    if err := mdt.AddDatabase("orders", "orders.ldb"); err != nil {
        return err
    }
    
    // 开始事务
    if err := mdt.Begin(); err != nil {
        return err
    }
    
    // 获取用户数据库事务
    userDB, err := mdt.GetDB("users")
    if err != nil {
        mdt.Rollback()
        return err
    }
    
    // 获取订单数据库事务
    orderDB, err := mdt.GetDB("orders")
    if err != nil {
        mdt.Rollback()
        return err
    }
    
    // 在各自的事务中操作
    user := &User{Name: "John", Email: "john@example.com"}
    if err := userDB.Create(user).Error; err != nil {
        mdt.Rollback()
        return err
    }
    
    order := &Order{UserID: user.ID, Amount: 100.0}
    if err := orderDB.Create(order).Error; err != nil {
        mdt.Rollback()
        return err
    }
    
    // 提交所有事务
    return mdt.Commit()
}


// 在您的sqlite.go中添加多库事务支持
type SqliteManager struct {
    instances map[string]*Sqlite
    multiTx   *MultiDBTransaction
    mu        sync.RWMutex
}

var sqliteManager = &SqliteManager{
    instances: make(map[string]*Sqlite),
}

func GetSqliteInstance(dbName string) *Sqlite {
    sqliteManager.mu.RLock()
    if instance, exists := sqliteManager.instances[dbName]; exists {
        sqliteManager.mu.RUnlock()
        return instance
    }
    sqliteManager.mu.RUnlock()
    
    sqliteManager.mu.Lock()
    defer sqliteManager.mu.Unlock()
    
    // 双重检查
    if instance, exists := sqliteManager.instances[dbName]; exists {
        return instance
    }
    
    instance := NewSqlite()
    instance.Name = dbName
    sqliteManager.instances[dbName] = instance
    return instance
}

func BeginMultiDBTransaction(dbNames ...string) (*MultiDBTransaction, error) {
    mdt := NewMultiDBTransaction()
    
    for _, dbName := range dbNames {
        instance := GetSqliteInstance(dbName)
        path, err := instance.getPath()
        if err != nil {
            return nil, err
        }
        
        if err := mdt.AddDatabase(dbName, path); err != nil {
            return nil, err
        }
    }
    
    if err := mdt.Begin(); err != nil {
        return nil, err
    }
    
    return mdt, nil
}


SQLite原生限制：不支持跨文件事务
ATTACH方案：可以在单个连接中操作多个数据库文件，支持真正的ACID事务
多DB管理器：模拟分布式事务，但不是真正的原子性
推荐方案：对于强一致性要求，使用ATTACH方案；对于松散耦合场景，使用多DB管理器