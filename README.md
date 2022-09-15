# digitalway.hk Core 

[简体中文] 


## 开发背景及目标

本方案不是技术框架，技术层面方案引用了多种优秀的技术框架和组件，本方案需要解决的问题是业务开发混乱的问题，适用于商业的业务开发场景，定义标准的商业开发过程，在这个过程中抽象和标准化实现商业功能
本方案的默认实现基于接口，修改默认约定可以通过重写接口的方式实现，本方案大多情况都是在约束开发者行为，要求用标准固定的方式开发功能，采用先有后优，异构编写进行项目开发
业务和技术实现分离，在业务实现功能时，不应考虑技术细节，业务功能实现和技术实现隔离，标准业务功能引用的技术实现完全可替代，可单独升级和迭代，互相不影响，例如：业务实现只决定数据的存储时机，技术实现决定存储的方式，只要存储时机不改变，存储的技术方案修改（分库、分表）不应该影响业务已经实现的功能
方案下的每种实现都不一定是最优方式，希望可以不断改进为更加优秀的方式替换

## 引用框架和技术组件
   * 引用的框架和技术组件，增加引用的请在本处更新指引
   * 数据库操作[Gorm](https://github.com/go-gorm/gorm)[说明文档](https://gorm.io/zh_CN/docs/constraints.html)
   * 缓存操作[BoltDB](https://github.com/boltdb/bolt)
   * Http服务[Go-Zero](https://github.com/zeromicro/go-zero)[说明文档](https://go-zero.dev/cn/) 
   * WebSocket服务[Socket.io](https://github.com/googollee/go-socket.io) 
   * 网络框架[EVIO](https://github.com/tidwall/evio)
   * 网络框架[gnet](https://github.com/panjf2000/gnet)
   * 数据验证[validator](https://github.com/go-playground/validator)
   * Swagger[kin-openapi](github.com/getkin/kin-openapi) [swagger-ui](https://github.com/swagger-api/swagger-ui)
   * 大浮点数[decimal](https://github.com/shopspring/decimal)
## 规范和要求
   * 编码规范[uber-go](https://github.com/xxjwxc/uber_go_guide_cn#uber-goguide-%E7%9A%84%E4%B8%AD%E6%96%87%E7%BF%BB%E8%AF%91)
   * 项目规范[golang-standards](https://github.com/golang-standards/project-layout/blob/master/README_zh.md)
   * go版本要求 > 1.18
## 安装引用
    go get github.com/digitalwayhk/core@latest
## 快速开始
    演示如何创建简单的订单功能api，供前端调用  
    在任意位置创建新目录demo，在demo目录下，初始化go项目, 终端中运行 go mod init demo
    在demo目录下，创建api、main、models目录,api目录下创建private和public目录
![效果](/docs/readmeimg/dic.jpg)  
    在目录中创建以下文件，并copy代码，完成后,终端中运行 go mod tidy 获取引用

### 1、创建demo/models 文件夹下创建order.go文件,并在order.go文件中定义OrderModel,如下代码所示
            
           package models

            import (
                "github.com/digitalwayhk/core/pkg/persistence/entity"

                "github.com/shopspring/decimal"
            )

            //OrderModel 订单模型
            type OrderModel struct {
                *entity.Model                 //从基础Model继承，默认添加ID,创建时间和状态字段
                UserID        uint            //用户ID
                Price         decimal.Decimal //价格
                Amount        decimal.Decimal //数量
            }

            //NewOrderModel 新建订单模型
            func NewChainModel() *OrderModel {
                return &OrderModel{
                    Model: entity.NewModel(),
                }
            }

            //NewOrderModel 新建订单模型，用于ModelList的NewItem方法
            func (own *OrderModel) NewModel() {
                if own.Model == nil {
                    own.Model = entity.NewModel()
                }
            }

### 2、创建demo/api,在api文件夹下创建private和public文件夹,在private文件夹下创建addorder.go文件用于创建新增order接口，在public文件夹下创建getorder.go文件用于创建获取order接口
            demo
             |-api
             |---private
             |-----addorder.go    //私的接口 api/addorder 必须使用有用户token的请求调用
             |---public
             |-----getorder.go    //公共接口 api/getorder 可以直接调用
     
### 3、创建demo/main文件夹,在这个文件夹下创建main.go文件用于创建服务运行程序，目录和文件结构如下所示
       
           demo
             |-main
             |---main.go
    
### 4、在addorder.go、getorder.go 中实现IRouter接口，copy以下代码到对应文件
      
* addorder.go 代码

        package private

        import (
            "github.com/digitalwayhk/core/demo/models"
            "github.com/digitalwayhk/core/pkg/persistence/entity"
            "github.com/digitalwayhk/core/pkg/server/router"
            "github.com/digitalwayhk/core/pkg/server/types"
            "errors"

            "github.com/shopspring/decimal"
        )

        //AddOrder 新增订单
        type AddOrder struct {
            Price  string `json:"price"`  //价格
            Amount string `json:"amount"` //数量
        }

        //解析Order参数
        func (this *AddOrder) Parse(req types.IRequest) error {
            return req.Bind(this)
        }

        //验证新增Ordre是否允许调用,该方法返回nil，Do方法将被调用
        func (this *AddOrder) Validation(req types.IRequest) error {
            if this.Price == "" {
                return errors.New("price is empty")
            }
            price, err := decimal.NewFromString(this.Price)
            if err != nil {
                return errors.New("price is not decimal")
            }
            if price.LessThan(decimal.NewFromFloat(0)) {
                return errors.New("price is less than zero")
            }
            if this.Amount == "" {
                return errors.New("amount is empty")
            }
            amount, err := decimal.NewFromString(this.Amount)
            if err != nil {
                return errors.New("amount is not decimal")
            }
            if amount.LessThan(decimal.NewFromFloat(0)) {
                return errors.New("amount is less than zero")
            }
            if u, _ := req.GetUser(); u != 0 {
                return errors.New("userid is empty")
            }
            return nil
        }

        //执行新增order逻辑,保存数据
        func (this *AddOrder) Do(req types.IRequest) (interface{}, error) {
            //创建model容器
            list := entity.NewModelList[models.OrderModel](nil)
            //新建order
            order := list.NewItem()
            //获取token中的userid
            order.UserID, _ = req.GetUser()
            price, _ := decimal.NewFromString(this.Price)
            //设置价格
            order.Price = price
            amount, _ := decimal.NewFromString(this.Amount)
            //设置数量
            order.Amount = amount
            //添加到容器
            err := list.Add(order)
            if err != nil {
                return nil, err
            }
            //持久化到数据库
            err = list.Save()
            return order, err
        }

        //RouterInfo路由注册信息
        func (this *AddOrder) RouterInfo() *types.RouterInfo {
            //设置默认路由信息
            return router.DefaultRouterInfo(this)
        }

* getorder.go 代码 

        package public

        import (
            "github.com/digitalwayhk/core/demo/models"
            "github.com/digitalwayhk/core/pkg/persistence/entity"
            "github.com/digitalwayhk/core/pkg/server/router"
            "github.com/digitalwayhk/core/pkg/server/types"
        )

        type GetOrder struct {
        }

        //解析Order参数
        func (this *GetOrder) Parse(req types.IRequest) error {
            return nil
        }

        //验证新增Ordre是否允许调用,该方法返回nil，Do方法将被调用
        func (this *GetOrder) Validation(req types.IRequest) error {
            return nil
        }

        //执行新增order逻辑,保存数据
        func (this *GetOrder) Do(req types.IRequest) (interface{}, error) {
            //创建model容器
            list := entity.NewModelList[models.OrderModel](nil)
            //获取默认查询参数
            item := list.GetSearchItem()
            //查询数据
            err := list.LoadList(item)
            return list.ToArray(), err
        }

        //RouterInfo路由注册信息
        func (this *GetOrder) RouterInfo() *types.RouterInfo {
            //设置默认路由信息
            return router.DefaultRouterInfo(this)
        }   

### 5、修改main.go文件，创建运行服务

* main.go 代码

        package main

        import (
            "github.com/digitalwayhk/core/demo/api/private"
            "github.com/digitalwayhk/core/demo/api/public"
            "github.com/digitalwayhk/core/pkg/server/run"
            "github.com/digitalwayhk/core/pkg/server/types"
        )

        //OrderService 订单服务
        type OrderService struct {
        }

        func (own *OrderService) ServiceName() string {
            return "orders"
        }
        func (own *OrderService) Routers() []types.IRouter {
            //添加已实现路由添加到OrderService的路由表，用于在Service中自动注册
            routers := []types.IRouter{
                &public.GetOrder{},
                &private.AddOrder{},
            }
            return routers
        }
        func (own *OrderService) SubscribeRouters() []*types.ObserveArgs {
            return []*types.ObserveArgs{}
        }

        func main() {
            //创建WebServer实例
            server := run.NewWebServer()
            //添加OrderService服务
            server.AddIService(&OrderService{})
            //启动服务
            server.Start()
        }   
      
### 6、编译运行，执行编译命令 go build -o ./demo/cmd/order ./demo/main  ，将在demo/cmd文件夹生在order执行文件，如下

    demo
     |- cmd
         |- order

### 7、运行执行程序，在终端的cmd目录执行./demo/cmd/order,通过-help可以查阅启动可用参数，服务启动

    -p int
        运行端口,默认8080 (default 8080)
    -server string
            主服务器地址,当前服务器的父服务器地址,如果是根服务器，则不需要此参数    //当部署多个服务实例时，需要该参数指定根服务ip地址，多服务时自动同步配置和运行信息
    -socket int
            启用Socket服务并指定端口,为0时不启用Socket服务 (default 7070)     //服务间内部通讯默认端口，当关闭时，服务间通讯走http方式
    -view int
            启用视图服务并指定端口,为0时不启用视图服务 (default 80)            //开发视图服务，用于在开发过程中可视化接口信息和服务运行信息，并可使用管理接口生成的ui界面设置业务数据，用于api功能测试
    
   
![运行的效果](/docs/readmeimg/orderrun.jpg)
 在启动的服务中，server服务为框架内置服务，用于服务运行信息和远程数据库信息管理
 orders 服务为开发的服务，注册新开发的 /api/orders/getorder和/api/orders/addordder路由，默认端口为8081，默认路由是POST注册，只能被POST访问
 orders 服务中的servermanage路由为框架自带，每个服务默认会带这些路由，用于服务本身的管理，可以通过在OrderService中实现ICloseServerManage接口关闭

### 打开postmen,测试接口运行，接口支持rest和websocket二种调用方式

post访问127.0.0.1:8081/api/orders/getorder 

![效果](/docs/readmeimg/getorder.jpg)

注意，必须用POST方式调用，可以看到返回的data为[]，没有返回数据


post 访问 127.0.0.1:8081/api/orders/addorder

![效果](/docs/readmeimg/addorder_not.jpg)
输入json参数
 {
    "price":"3443.3443",
    "amount":"453"
 }
调用，返回401错误，没有授权，因发布在private目录api默认需要token才能访问

post 访问 127.0.0.1:8081/api/servermanage/testtoken?userid=12345 ，获取测试token

![效果](/docs/readmeimg/testtoken.jpg)

post 访问 127.0.0.1:8081/api/orders/addorder 填入获取的token,调用返回成功

![效果](/docs/readmeimg/addorder_yes.jpg)

post访问127.0.0.1:8080/api/order/getorder 获取添加的order信息

![效果](/docs/readmeimg/getadd.jpg)

websocket 订阅getorder,获取order信息

1、连接到 ws://127.0.0.1:8081/ws

2、发送订阅消息 
{
    "channel":"/api/orders/getorder",
    "event":"sub"
}

3、退订，发送相同参数，event参数修改为unsub

![效果](/docs/readmeimg/websocketsub.jpg)
![效果](/docs/readmeimg/websocketunsub.jpg)

### 开发视图，打开浏览器，输入http://localhost 进入开发视图服务，开发视图默认运行在80端口，当80端口占用时，需要使用view参数修改启动端口

![效果](/docs/readmeimg/view.jpg)

### 管理模块功能演示,为订单增加token属性，token应该由管理员维护，对用户只读，下边例子完成该功能

   打开models/order.go文件，在ordermodel中增加token属性

    type OrderModel struct {
	*entity.Model                 //从基础Model继承，默认添加ID,创建时间和状态字段
	UserID        uint            //用户ID
	Price         decimal.Decimal //价格
	Amount        decimal.Decimal //数量
	Token         uint            //币种
}
   在models目录，新增token.go文件，copy以下代码到token.go文件
    
    package models

    import "github.com/digitalwayhk/core/pkg/persistence/entity"

    type TokenModel struct {
        *entity.Model
        Name string
    }

    //NewTokenModel 新建币种模型
    func NewTokenModel() *TokenModel {
        return &TokenModel{
            Model: entity.NewModel(),
        }
    }

    //NewTokenModel 新建币种模型，用于ModelList的NewItem方法
    func (own *TokenModel) NewModel() {
        if own.Model == nil {
            own.Model = entity.NewModel()
        }
    }
  在api目录，新增加manage目录,在manage目录中创建tokenmanage.go文件，copy以下代码到tokenmanage.go文件中
    
    package manage

    import (
        "demo/models"

        "github.com/digitalwayhk/core/service/manage"
    )

    type TokenManage struct {
        *manage.ManageService[models.TokenModel]
    }

    func NewTokenManage() *TokenManage {
        own := &TokenManage{}
        own.ManageService = manage.NewManageService[models.TokenModel](own)
        return own
    }
  在main.go文件中的OrderService中的Routers方法中，添加TokenManage路由

    routers = append(routers, manage.NewTokenManage().Routers()...)
![效果](/docs/readmeimg/addmanagerouter.jpg)

    再次编译运行服务，运行后，打开开发视图http://localhost，左侧菜单增加orders主菜单，orders下增加了TokenManage菜单，如下图
![效果](/docs/readmeimg/addmanagerun.jpg)
    
    TokenModel已经可以增删改查，可以直接使用

![效果](/docs/readmeimg/addmanageedit.jpg)




      

## 业务开发标准
       开发工作是一种脑力劳动，开发完成前的过程和结果大部分在开发人员的脑海中存在。个人编程技能的差异，需求和任务的理解能力差异，这些都会因人而亦，这会导致最终结果会有较大的随机性，从效率和质量上都难以客观衡量，从长期需要不断研进的任务来讲，时间和人员的增加一定导致质量和效率的降低，随着功能叠加，用户使用的增加，资源依赖逐渐加重，事故频率也会不断提高，问题几乎无法有效改善，只能通过追加人手和资源的方式人肉运维。
       定义开发标准就是以我们自身的大部分开发任务为基准，在一定范围内限定开发的自由性，以定义的默认约定或约束来规范开发行为，形成一些标准实现，以可迭代和替换的方式逐步优化技术方案，不断的对已实现标准进行改进，同时又不影响业务功能，从而保证开发结果是在往更为良性的方向前进。其主要手段应用在分析、开发、上线三个过程，方式就是定义或实现在这三个过程里的动作和产出物

### 分析标准
       输入产品的需求说明或原型文档
       输出接口文档（几个接口，接口输入输出）
       标准分析过程
        1、分析出使用数据模型（现有、新建、修改）和模型结构间关系（一对一，一对多，多对多）
            基础模型 (增长只和本身有关，使用后不可删除，例如，国家、区块链等)
            单据模型（增长使用直接相关，有状态变更，例如，订单、账单等）
            记录模型 (增长使用相关，只有新增和删除，无更新，无状态，可按时间拆分)
            分析模型 (统计类模型，分纬度和指标二种)
        2、分析出使用业务服务（现有、新建、修改）和调用关系（同步、异步、订阅）
            管理服务
            公共服务
            私有服务
        3、定义API
            所在服务
            API类型和参数
            输出接口（对相关使用方）    
### 开发标准
       输入分析结果
       输出测试环境运行结果(符合测试标准的编译程序)
       标准开发过程
        1、按分析结果开发model
           继承关系
           定义属性
           个性验证
           个性存储
        2、开发service,实现业务功能
            调用model
            调用内部service
            调用外部service
            订阅service
        3、开发api,发布API给web、app其他任何调用方使用
            解析参数
            验证异常
            调用业务
        4、开发单元测试
            模糊测试
            单元测试
            基准测试
### 上线标准
       输入编译程序到测试环境
       输出编译程序到生产环境运行（用户使用的功能）
       标准上线过程
       1、测试上线
          功能测试（+自动化）
          性能测试
          回归测试
       2、运维上线
          测试环境同步运行程序到生产环境
          个性配置生产运行技术参数
          个性配置生产数据库连接
       3、产品上线
          产品业务功能设置
          灰度发布（范围限定）
          正式发布

