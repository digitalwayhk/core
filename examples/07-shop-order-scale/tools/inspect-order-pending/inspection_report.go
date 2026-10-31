// 本文件定义离线物理核对结果，不冒充运行期缓存计数或远端事实核验。
package main

type inspectionReport struct {
	Format   string           `json:"format"`
	ReadOnly bool             `json:"readOnly"`
	Facts    int64            `json:"facts"`
	Queues   int64            `json:"queues"`
	Unsynced int64            `json:"unsynced"`
	Issues   map[string]int64 `json:"issues"`
}
