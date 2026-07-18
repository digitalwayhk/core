package types

import (
	"fmt"
	"reflect"
	"runtime"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

// 🔧 使用 channel 实现的对象池（更安全）
type ChannelPool struct {
	pool    chan IRouter
	factory func() IRouter
	maxSize int
}

func NewChannelPool(factory func() IRouter, size int) *ChannelPool {
	return &ChannelPool{
		pool:    make(chan IRouter, size),
		factory: factory,
		maxSize: size,
	}
}

func (p *ChannelPool) Get() IRouter {
	select {
	case router := <-p.pool:
		return router
	default:
		// 池为空，创建新对象
		return p.factory()
	}
}

func (p *ChannelPool) Put(router IRouter) {
	if router == nil {
		return
	}

	select {
	case p.pool <- router:
		// 成功放回池中
	default:
		// 池已满，丢弃对象
	}
}

// 🔧 在 RouterInfo 中使用 ChannelPool
func (own *RouterInfo) initChannelPool() {
	if own.channelPool != nil {
		return
	}
	own.Lock()
	defer own.Unlock()
	if own.channelPool != nil {
		return
	}
	// 已持有 RouterInfo 写锁，直接读取注册期快照字段，避免 Getter 重入 RWMutex。
	size := own.PoolSize
	if size <= 0 {
		size = defaultPoolSize()
	}
	own.channelPool = NewChannelPool(own.newRouterInstance, size)

}

// newRouterInstance 通过路由工厂或注册类型创建一个独立实例。
// 该方法只负责创建，不决定实例属于请求池还是长期订阅。
func (own *RouterInfo) newRouterInstance() IRouter {
	if factory, ok := own.instance.(IRouterFactory); ok {
		return factory.New(own.instance)
	}
	instance := utils.NewInterface(own.instance)
	if instance == nil {
		logx.Errorf("Failed to create new instance for %s", own.Path)
		return nil
	}
	router, ok := instance.(IRouter)
	if !ok {
		logx.Errorf("Created instance for %s does not implement IRouter", own.Path)
		return nil
	}
	return router
}

// defaultPoolSize 返回基于 GOMAXPROCS 的默认对象池大小。
// 对 I/O 密集型 Handler，乘以 4 可减少 GC 频率；最小 16，最大 256。
func defaultPoolSize() int {
	n := runtime.GOMAXPROCS(0) * 4
	if n < 16 {
		n = 16
	}
	if n > 256 {
		n = 256
	}
	return n
}

func (own *RouterInfo) getNew() IRouter {
	defer func() {
		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:", own.ServiceName, own.Path), err)
		}
	}()

	own.initChannelPool()

	router := own.channelPool.Get()
	if router == nil {
		return utils.NewInterface(own.instance).(IRouter)
	}

	// 🔧 从 channel 池获取的对象天然是独占的，不需要额外加锁
	own.resetRouter(router)

	return router
}

// 🔧 新增：重置路由对象状态（通用版本）
func (own *RouterInfo) resetRouter(router IRouter) {
	// 优先使用自定义重置接口
	if resettable, ok := router.(IRouterResettable); ok {
		resettable.Reset()
		return
	}

	// 使用通用反射重置
	own.genericReset(router)
}

// 🔧 通用反射重置函数
func (own *RouterInfo) genericReset(router IRouter) {
	if router == nil {
		return
	}

	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("genericReset panic: %v", err)
		}
	}()

	v := reflect.ValueOf(router)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if !v.IsValid() || !v.CanSet() {
		return
	}

	t := v.Type()

	// 遍历所有字段
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// 跳过未导出的字段
		if !field.CanSet() {
			continue
		}

		// 跳过嵌入字段（如 *entity.Model）
		if fieldType.Anonymous {
			continue
		}

		// 根据字段类型重置
		switch field.Kind() {
		case reflect.String:
			field.SetString("")

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(0)

		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			field.SetUint(0)

		case reflect.Float32, reflect.Float64:
			field.SetFloat(0)

		case reflect.Bool:
			field.SetBool(false)

		case reflect.Slice:
			// 清空切片但保留容量
			if !field.IsNil() {
				field.SetLen(0)
			}

		case reflect.Map:
			// 清空map
			if !field.IsNil() {
				field.Set(reflect.MakeMap(field.Type()))
			}

		case reflect.Ptr:
			// 指针类型设为nil（除了嵌入的Model）
			if !field.IsNil() && !fieldType.Anonymous {
				field.Set(reflect.Zero(field.Type()))
			}

		case reflect.Interface:
			// 接口类型设为nil
			if !field.IsNil() {
				field.Set(reflect.Zero(field.Type()))
			}

		case reflect.Struct:
			// 结构体类型递归重置
			if field.CanAddr() {
				own.resetStructField(field)
			}
		}
	}
}

// 🔧 递归重置结构体字段
func (own *RouterInfo) resetStructField(v reflect.Value) {
	if !v.CanSet() {
		return
	}

	t := v.Type()

	// 特殊处理：time.Time 设为零值
	if t.String() == "time.Time" {
		v.Set(reflect.Zero(t))
		return
	}

	// 递归处理其他结构体字段
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.CanSet() {
			field.Set(reflect.Zero(field.Type()))
		}
	}
}

// 🔧 新增：回收路由对象到对象池
func (own *RouterInfo) putRouter(router IRouter) {
	if router == nil {
		return
	}

	defer func() {
		if err := recover(); err != nil {
			logx.Error("Put router to pool failed:", err)
		}
	}()

	// 🔧 清理敏感数据
	own.cleanRouter(router)

	// 🔧 放回创建和获取所使用的同一个有界对象池
	if own.channelPool != nil {
		own.channelPool.Put(router)
	}
}

// releaseSubscription 清理长期 WebSocket 订阅实例，但不将它放入短期请求对象池。
func (own *RouterInfo) releaseSubscription(router IRouter) {
	if router == nil {
		return
	}
	own.cleanRouter(router)
}

// 🔧 新增：清理路由对象的敏感数据
func (own *RouterInfo) cleanRouter(router IRouter) {
	// 如果实现了清理接口，调用清理方法
	if cleanable, ok := router.(IRouterCleanable); ok {
		cleanable.Clean()
	}
}
