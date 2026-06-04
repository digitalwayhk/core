package adminfullstack

// FieldSchema 说明前端 WayForm / WayTable 需要的字段信息。
// 实际项目请使用 service/manage/view.FieldModel。
type FieldSchema struct {
	Field   string `json:"field"`   // 后端模型字段名
	Title   string `json:"title"`   // 前端显示标题
	Visible bool   `json:"visible"` // 是否在表格中显示
	IsEdit  bool   `json:"isedit"`  // 是否在表单中编辑
	Type    string `json:"type"`    // 字段类型，例如 string、number、date
}

// CommandSchema 说明管理页面上的命令按钮。
// 实际项目请使用 service/manage/view.CommandModel。
type CommandSchema struct {
	Command     string `json:"command"`     // add/edit/remove/submit/release
	Title       string `json:"title"`       // 按钮标题
	IsSelectRow bool   `json:"isselectrow"` // 是否要求先选中一行
}

// ViewSchema 说明 View 接口返回给 WayPage 的页面模型。
// 实际项目请使用 service/manage/view.ViewModel。
type ViewSchema struct {
	Name     string          `json:"name"`     // 模型名称
	Title    string          `json:"title"`    // 页面标题
	AutoLoad bool            `json:"autoload"` // 是否进入页面后自动 Search
	Fields   []FieldSchema   `json:"fields"`   // 表格和表单字段
	Commands []CommandSchema `json:"commands"` // 操作按钮
}

// TableData 说明 Search 接口返回给 WayTable 的列表数据。
// 实际项目请使用 service/manage/view.TableData。
type TableData struct {
	Rows  interface{} `json:"rows"`  // 当前页数据
	Total int64       `json:"total"` // 总条数
}
