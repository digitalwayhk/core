package stats

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ReportKind 报表展示类型（前端据此渲染图表/表格）。
type ReportKind string

const (
	ReportKindBar   ReportKind = "bar"   // 柱状
	ReportKindLine  ReportKind = "line"  // 折线
	ReportKindPie   ReportKind = "pie"   // 饼图
	ReportKindTable ReportKind = "table" // 表格
	ReportKindMixed ReportKind = "mixed" // 图 + 表 + 排名
)

// ReportDef 报表元数据（与 Dashboard 分离；挂在服务子菜单下）。
// Path 约定前端：/report/{service}/{code}
type ReportDef struct {
	// Code 服务内唯一，如 order-daily
	Code string `json:"code"`
	// Service 服务名，如 shop-order
	Service string `json:"service"`
	// Title 菜单与页标题
	Title string `json:"title"`
	// Description 说明
	Description string `json:"description,omitempty"`
	// Kind 主图表类型
	Kind ReportKind `json:"kind"`
	// SpecCode 绑定的 StatSpec.Code（数据源）
	SpecCode string `json:"specCode"`
	// MetricAlias 主指标（默认 row_count 或 Spec 第一项）；多指标时用 MetricAliases
	MetricAlias string `json:"metricAlias,omitempty"`
	// MetricAliases 多序列指标（如 ["row_count","amount_sum"] 画双折线）；优先于 MetricAlias
	MetricAliases []string `json:"metricAliases,omitempty"`
	// MetricTitles 指标业务名，key 为 alias（如 row_count→订单笔数）
	MetricTitles map[string]string `json:"metricTitles,omitempty"`
	// DimAlias 主维度（空=仅时间轴）
	DimAlias string `json:"dimAlias,omitempty"`
	// ChartTitle 图表卡片标题，空则用 Title
	ChartTitle string `json:"chartTitle,omitempty"`
	// TableTitle 明细表标题，空则「明细」
	TableTitle string `json:"tableTitle,omitempty"`
	// MenuTitle 菜单文案，空则用 Title
	MenuTitle string `json:"menuTitle,omitempty"`
	// Sort 菜单排序，小在前
	Sort int `json:"sort"`
	// DrillReportCode 点击图后进入的下一级报表 code（同服务）
	DrillReportCode string `json:"drillReportCode,omitempty"`
	// LinkManagePath 跳转 Manage 列表路径，如 /main/shop-order/ordermanage
	LinkManagePath string `json:"linkManagePath,omitempty"`
	// LinkLabel 跳转按钮文案
	LinkLabel string `json:"linkLabel,omitempty"`
}

// ReportMenuItem 供菜单与列表 API 使用。
type ReportMenuItem struct {
	Code        string `json:"code"`
	Service     string `json:"service"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Path 前端路由
	Path string `json:"path"`
	// Kind 图表类型
	Kind ReportKind `json:"kind"`
	Sort int        `json:"sort"`
}

// ReportView 单报表完整数据（页面渲染）。
type ReportView struct {
	Def        ReportDef `json:"def"`
	ComputedAt string    `json:"computedAt,omitempty"`
	// Series 主图序列
	Series []ChartValue `json:"series"`
	// Table 明细表（列由 headers + rows）
	TableHeaders []string            `json:"tableHeaders"`
	TableRows    []map[string]string `json:"tableRows"`
	// Ranking 侧栏排名（可选）
	Ranking []RankingValue `json:"ranking,omitempty"`
	// Summary 页头摘要
	Summary []StatisticItem `json:"summary,omitempty"`
	// Empty 无快照数据
	Empty   bool   `json:"empty"`
	Message string `json:"message,omitempty"`
}

// ReportPath 前端路径。
func ReportPath(service, code string) string {
	return "/report/" + strings.Trim(service, "/") + "/" + strings.Trim(code, "/")
}

// MenuTitleOf 菜单标题。
func (d ReportDef) MenuTitleOf() string {
	if strings.TrimSpace(d.MenuTitle) != "" {
		return d.MenuTitle
	}
	return d.Title
}

// ToMenuItem 转菜单项。
func (d ReportDef) ToMenuItem() ReportMenuItem {
	return ReportMenuItem{
		Code:        d.Code,
		Service:     d.Service,
		Title:       d.MenuTitleOf(),
		Description: d.Description,
		Path:        ReportPath(d.Service, d.Code),
		Kind:        d.Kind,
		Sort:        d.Sort,
	}
}

var (
	reportMu   sync.RWMutex
	reportDefs = map[string]ReportDef{} // key: service/code
)

func reportKey(service, code string) string {
	return strings.TrimSpace(service) + "/" + strings.TrimSpace(code)
}

// RegisterReports 注册报表定义（启动期；code 在服务内唯一）。
func RegisterReports(defs ...ReportDef) {
	reportMu.Lock()
	defer reportMu.Unlock()
	for _, d := range defs {
		if err := validateReportDef(d); err != nil {
			panic(fmt.Sprintf("stats.RegisterReports: %v", err))
		}
		k := reportKey(d.Service, d.Code)
		if _, ok := reportDefs[k]; ok {
			panic(fmt.Sprintf("stats.RegisterReports duplicate %s", k))
		}
		reportDefs[k] = d
	}
}

// GetReportDef 取定义。
func GetReportDef(service, code string) (ReportDef, bool) {
	reportMu.RLock()
	defer reportMu.RUnlock()
	d, ok := reportDefs[reportKey(service, code)]
	return d, ok
}

// ListReportDefs 列出服务下全部报表（按 Sort）。
func ListReportDefs(service string) []ReportDef {
	reportMu.RLock()
	defer reportMu.RUnlock()
	service = strings.TrimSpace(service)
	out := make([]ReportDef, 0)
	for _, d := range reportDefs {
		if service == "" || d.Service == service {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort == out[j].Sort {
			return out[i].Code < out[j].Code
		}
		return out[i].Sort < out[j].Sort
	})
	return out
}

// ListReportMenus 菜单列表。
func ListReportMenus(service string) []ReportMenuItem {
	defs := ListReportDefs(service)
	out := make([]ReportMenuItem, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.ToMenuItem())
	}
	return out
}

// ResetReportsForTest 清空报表注册（仅测试）。
func ResetReportsForTest() {
	reportMu.Lock()
	defer reportMu.Unlock()
	reportDefs = map[string]ReportDef{}
}

func validateReportDef(d ReportDef) error {
	if strings.TrimSpace(d.Code) == "" {
		return fmt.Errorf("code 不能为空")
	}
	if strings.TrimSpace(d.Service) == "" {
		return fmt.Errorf("service 不能为空")
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("title 不能为空")
	}
	if strings.TrimSpace(d.SpecCode) == "" {
		return fmt.Errorf("specCode 不能为空")
	}
	switch d.Kind {
	case ReportKindBar, ReportKindLine, ReportKindPie, ReportKindTable, ReportKindMixed, "":
	default:
		return fmt.Errorf("未知 kind: %s", d.Kind)
	}
	return nil
}

// ChartTitleOf 图表卡片标题。
func (d ReportDef) ChartTitleOf() string {
	if strings.TrimSpace(d.ChartTitle) != "" {
		return d.ChartTitle
	}
	return d.Title
}

// TableTitleOf 明细表标题。
func (d ReportDef) TableTitleOf() string {
	if strings.TrimSpace(d.TableTitle) != "" {
		return d.TableTitle
	}
	return "明细"
}

// ResolveMetrics 解析要展示的指标列表（有序）。
func (d ReportDef) ResolveMetrics(snap Snapshot) []string {
	if len(d.MetricAliases) > 0 {
		out := make([]string, 0, len(d.MetricAliases))
		for _, m := range d.MetricAliases {
			m = strings.TrimSpace(m)
			if m != "" {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if strings.TrimSpace(d.MetricAlias) != "" {
		return []string{strings.TrimSpace(d.MetricAlias)}
	}
	// 默认：优先 row_count + amount_sum（若存在），否则第一个 metric
	if len(snap.Rows) == 0 {
		return []string{"row_count"}
	}
	metrics := snap.Rows[0].Metrics
	out := make([]string, 0, 2)
	if _, ok := metrics["row_count"]; ok {
		out = append(out, "row_count")
	}
	if _, ok := metrics["amount_sum"]; ok {
		out = append(out, "amount_sum")
	}
	if len(out) > 0 {
		return out
	}
	for k := range metrics {
		return []string{k}
	}
	return []string{"row_count"}
}

// MetricTitleOf 指标业务展示名。
func (d ReportDef) MetricTitleOf(alias string) string {
	if d.MetricTitles != nil {
		if t := strings.TrimSpace(d.MetricTitles[alias]); t != "" {
			return t
		}
	}
	switch alias {
	case "row_count":
		return "订单笔数"
	case "amount_sum":
		return "金额"
	case "qty_sum":
		return "销量"
	default:
		return alias
	}
}

// BuildReportView 从 StatsStore 快照组装报表视图（不查库）。
func BuildReportView(store *Store, service, code string) (ReportView, error) {
	def, ok := GetReportDef(service, code)
	if !ok {
		return ReportView{}, fmt.Errorf("报表不存在: %s/%s", service, code)
	}
	if def.Kind == "" {
		def.Kind = ReportKindMixed
	}
	view := ReportView{Def: def}
	if store == nil {
		store = DefaultStore
	}
	snap, ok := store.Get(def.SpecCode)
	if !ok || len(snap.Rows) == 0 {
		view.Empty = true
		view.Message = "统计快照尚未就绪，请稍后刷新或触发 StatsRunner"
		view.ComputedAt = ""
		return view, nil
	}
	view.ComputedAt = snap.ComputedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	metrics := def.ResolveMetrics(snap)
	primary := metrics[0]

	view.Series = multiSeriesFromSnapshot(snap, def.DimAlias, metrics, def)
	view.TableHeaders, view.TableRows = multiTableFromSnapshot(snap, def.DimAlias, metrics, def)
	view.Ranking = rankingFromSnapshot(snap, def.DimAlias, primary, 10)
	// 摘要卡片：各业务指标合计（不是「快照行数」）
	view.Summary = make([]StatisticItem, 0, len(metrics))
	for _, m := range metrics {
		view.Summary = append(view.Summary, StatisticItem{
			Code:        m,
			DataName:    def.MetricTitleOf(m),
			Description: m,
			Value:       sumMetricAlias(snap, m),
			ValueFormat: "number",
		})
	}
	return view, nil
}

func multiSeriesFromSnapshot(snap Snapshot, dimAlias string, metrics []string, def ReportDef) []ChartValue {
	if len(metrics) == 0 {
		return nil
	}
	// 有维度：仍按主指标做分类汇总（饼/柱/排名）
	if strings.TrimSpace(dimAlias) != "" {
		return seriesFromSnapshot(snap, dimAlias, metrics[0], def.MetricTitleOf(metrics[0]))
	}
	// 无维度：每个指标一条时间序列（双折线）
	out := make([]ChartValue, 0)
	for _, metric := range metrics {
		title := def.MetricTitleOf(metric)
		part := seriesFromSnapshot(snap, "", metric, title)
		out = append(out, part...)
	}
	return out
}

func seriesFromSnapshot(snap Snapshot, dimAlias, metric, seriesName string) []ChartValue {
	// 无维度：按 bucket
	if strings.TrimSpace(dimAlias) == "" {
		m := map[string]float64{}
		for _, r := range snap.Rows {
			m[r.Bucket] += parseFloat(r.Metrics[metric])
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]ChartValue, 0, len(keys))
		for _, k := range keys {
			out = append(out, ChartValue{
				X:    formatBucketLabelSimple(k),
				Y:    fmt.Sprintf("%.0f", m[k]),
				Date: k,
				Name: seriesName,
			})
		}
		return out
	}
	// 有维度：汇总各 dim 总量（饼图/排名图）
	m := map[string]float64{}
	for _, r := range snap.Rows {
		label := dimLabel(r, dimAlias)
		m[label] += parseFloat(r.Metrics[metric])
	}
	type kv struct {
		k string
		v float64
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	out := make([]ChartValue, 0, len(list))
	for _, it := range list {
		out = append(out, ChartValue{X: it.k, Y: fmt.Sprintf("%.0f", it.v), Name: seriesName})
	}
	return out
}

func multiTableFromSnapshot(snap Snapshot, dimAlias string, metrics []string, def ReportDef) ([]string, []map[string]string) {
	if len(metrics) == 0 {
		return nil, nil
	}
	// 有维度：逐行明细（bucket + dim + 各指标）
	if strings.TrimSpace(dimAlias) != "" {
		headers := []string{"日期", "维度"}
		for _, m := range metrics {
			headers = append(headers, def.MetricTitleOf(m))
		}
		sorted := append([]StatRow(nil), snap.Rows...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Bucket == sorted[j].Bucket {
				return dimLabel(sorted[i], dimAlias) < dimLabel(sorted[j], dimAlias)
			}
			return sorted[i].Bucket < sorted[j].Bucket
		})
		rows := make([]map[string]string, 0, len(sorted))
		for _, r := range sorted {
			row := map[string]string{
				"日期": r.Bucket,
				"维度": dimLabel(r, dimAlias),
			}
			for _, m := range metrics {
				row[def.MetricTitleOf(m)] = r.Metrics[m]
			}
			rows = append(rows, row)
		}
		return headers, rows
	}
	// 无维度：按日期聚合各指标一列
	headers := []string{"日期"}
	for _, m := range metrics {
		headers = append(headers, def.MetricTitleOf(m))
	}
	// bucket -> metric -> sum
	type cell map[string]float64
	byBucket := map[string]cell{}
	for _, r := range snap.Rows {
		c, ok := byBucket[r.Bucket]
		if !ok {
			c = cell{}
			byBucket[r.Bucket] = c
		}
		for _, m := range metrics {
			c[m] += parseFloat(r.Metrics[m])
		}
	}
	keys := make([]string, 0, len(byBucket))
	for k := range byBucket {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		row := map[string]string{"日期": k}
		for _, m := range metrics {
			row[def.MetricTitleOf(m)] = fmt.Sprintf("%.0f", byBucket[k][m])
		}
		rows = append(rows, row)
	}
	return headers, rows
}

func rankingFromSnapshot(snap Snapshot, dimAlias, metric string, top int) []RankingValue {
	if dimAlias == "" {
		return nil
	}
	m := map[string]float64{}
	for _, r := range snap.Rows {
		m[dimLabel(r, dimAlias)] += parseFloat(r.Metrics[metric])
	}
	type kv struct {
		k string
		v float64
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	if top > 0 && len(list) > top {
		list = list[:top]
	}
	out := make([]RankingValue, 0, len(list))
	for i, it := range list {
		out = append(out, RankingValue{X: it.k, Y: fmt.Sprintf("%.0f", it.v), Rank: i + 1})
	}
	return out
}

func dimLabel(r StatRow, alias string) string {
	if alias == "" {
		return ""
	}
	dv, ok := r.Dims[alias]
	if !ok {
		return ""
	}
	if dv.Displays != nil {
		for _, k := range []string{"name", "productName", "supplierName", "productCode", "supplierCode", "code", "value"} {
			if v := strings.TrimSpace(dv.Displays[k]); v != "" {
				return v
			}
		}
		for _, v := range dv.Displays {
			if strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	if dv.ID > 0 {
		return fmt.Sprintf("#%d", dv.ID)
	}
	return alias
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f
}

func sumMetricAlias(snap Snapshot, metric string) string {
	var sum float64
	for _, r := range snap.Rows {
		sum += parseFloat(r.Metrics[metric])
	}
	return fmt.Sprintf("%.0f", sum)
}

func formatBucketLabelSimple(bucket string) string {
	if len(bucket) == 10 && bucket[4] == '-' {
		return bucket[5:]
	}
	if len(bucket) == 7 && bucket[4] == '-' {
		m := bucket[5:]
		if len(m) == 2 && m[0] == '0' {
			m = m[1:]
		}
		return m + "月"
	}
	return bucket
}
