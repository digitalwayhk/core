package stats

// Dashboard 是管理端 analysis 页的标准数据契约（与 web/admin data.d.ts 对齐）。
// 各服务 Manage 路由 POST /api/manage/{service}/analysis 应返回该结构。
type Dashboard struct {
	// Name 看板标识，如 shop-order-analysis
	Name string `json:"name"`
	// Service 服务名
	Service string `json:"service,omitempty"`
	// Title 页面标题（动态）
	Title string `json:"title,omitempty"`
	// Description 页面说明
	Description string `json:"description,omitempty"`
	// ComputedAt 数据刷新时间 RFC3339
	ComputedAt string `json:"computedAt,omitempty"`
	// Statistics 顶部卡片与分布序列（按 layout 或数组顺序展示）
	Statistics []StatisticItem `json:"statistics"`
	// Rankings 排行榜
	Rankings []RankingItem `json:"rankings"`
	// Categories 分类占比（饼图等）
	Categories []CategoryItem `json:"categories"`
	// TimeTrends 多序列时间趋势（门店/渠道对比）
	TimeTrends []TimeTrendItem `json:"timeTrends"`
	// Layout 前端布局提示：指标名与区块全部由服务端给出，避免前端写死中文
	Layout *DashboardLayout `json:"layout,omitempty"`
	// Query 后续下钻查询入口（可选）
	Query *DashboardQueryMeta `json:"query,omitempty"`
}

// DashboardLayout 指导 analysis 页如何绑定区块，名称均可动态。
type DashboardLayout struct {
	// IntroDataNames 顶部卡片使用的 statistics.dataName，按展示顺序
	IntroDataNames []string `json:"introDataNames"`
	// SalesTabLabel 主柱状图 Tab 名（如「销售额」「订单金额」）
	SalesTabLabel string `json:"salesTabLabel"`
	// VisitTabLabel 次 Tab 名（如「订单笔数」）
	VisitTabLabel string `json:"visitTabLabel"`
	// SalesSeriesName statistics 中柱状序列 dataName
	SalesSeriesName string `json:"salesSeriesName"`
	// VisitSeriesName 次序列 dataName
	VisitSeriesName string `json:"visitSeriesName"`
	// SalesRankingName rankings[].name
	SalesRankingName string `json:"salesRankingName"`
	// VisitRankingName rankings[].name
	VisitRankingName string `json:"visitRankingName"`
	// CategoryTitle 饼图标题
	CategoryTitle string `json:"categoryTitle,omitempty"`
	// OfflineSectionTitle 多门店/多渠道区标题
	OfflineSectionTitle string `json:"offlineSectionTitle,omitempty"`
}

// DashboardQueryMeta 后续按时间范围/粒度再查的标准接口说明。
type DashboardQueryMeta struct {
	// Path 相对 Manage 路径，如 /api/manage/shop-order/analysis/query
	Path string `json:"path,omitempty"`
	// SupportedGrains day|week|month|year
	SupportedGrains []string `json:"supportedGrains,omitempty"`
	// DefaultGrain
	DefaultGrain string `json:"defaultGrain,omitempty"`
}

// StatisticItem 顶部卡片或分布序列。
type StatisticItem struct {
	// Code 稳定键（可选，前端优先 dataName 展示）
	Code        string      `json:"code,omitempty"`
	DataName    string      `json:"dataName"`
	Description string      `json:"description"`
	Value       string      `json:"value"`
	ValueFormat string      `json:"valueFormat"`
	ValuePrefix string      `json:"valuePrefix"`
	ValueSuffix string      `json:"valueSuffix"`
	ChartData   *ChartData  `json:"chartData"`
	Trends      []TrendItem `json:"trends"`
	QueryURL    string      `json:"queryUrl,omitempty"`
}

// ChartData 迷你图或柱状序列。
type ChartData struct {
	Label     string       `json:"label"`
	Value     string       `json:"value"`
	MaxValue  string       `json:"maxValue"`
	ChartType string       `json:"chartType"` // area | bar | ring | progress
	Values    []ChartValue `json:"values"`
}

// ChartValue 图点。
type ChartValue struct {
	X    string `json:"x"`
	Y    string `json:"y"`
	Date string `json:"date"`
}

// TrendItem 同比/footer。
type TrendItem struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	Direction int    `json:"direction"` // 1 up, -1 down, 0 neutral/footer
}

// RankingItem 排行。
type RankingItem struct {
	Name     string         `json:"name"`
	QueryURL string         `json:"queryUrl"`
	Values   []RankingValue `json:"values"`
}

// RankingValue 排行项。
type RankingValue struct {
	X    string `json:"x"`
	Y    string `json:"y"`
	Rank int    `json:"rank"`
}

// CategoryItem 分类占比（可嵌套）。
type CategoryItem struct {
	Category string         `json:"category"`
	Value    string         `json:"value"`
	Total    string         `json:"total"`
	Children []CategoryItem `json:"children"`
}

// TimeTrendItem 多序列时间点。
type TimeTrendItem struct {
	X    string `json:"x"`
	Y    string `json:"y"`
	Name string `json:"name"`
	Date string `json:"date"`
}
