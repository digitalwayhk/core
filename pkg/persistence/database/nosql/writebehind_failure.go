// 本文件定义显式有界write-behind失败处置合同；不解释应用业务成功或金融顺序。
package nosql

import (
	"context"
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// ErrWriteBehindDisposition 表示失败处置合同、版本或持久状态不满足要求。
var ErrWriteBehindDisposition = errors.New("write-behind failure disposition rejected")

// ErrWriteBehindDispositionCloseTimeout 表示失败处置worker尚未实际退出，技术pending为零也不能报告关闭成功。
var ErrWriteBehindDispositionCloseTimeout = errors.New("write-behind disposition workers did not stop")

// WriteBehindFailurePolicy 是显式有限预算；MaxAttempts包含首次target调用，零值不启用新模式。
// Handler预算独立且不因重新通知、重启或人工恢复而清零。配置绑定后不可热修改。
type WriteBehindFailurePolicy struct {
	StoreID            string        `json:"store_id" yaml:"store_id"` // 应用提供的稳定服务/实例/store身份，迁移卷和重启必须保持
	MaxAttempts        int           `json:"max_attempts" yaml:"max_attempts"`
	MaxElapsed         time.Duration `json:"max_elapsed" yaml:"max_elapsed"`
	MinRetryInterval   time.Duration `json:"min_retry_interval" yaml:"min_retry_interval"`
	MaxRetryInterval   time.Duration `json:"max_retry_interval" yaml:"max_retry_interval"`
	HandlerMaxAttempts int           `json:"handler_max_attempts" yaml:"handler_max_attempts"`
	HandlerTimeout     time.Duration `json:"handler_timeout" yaml:"handler_timeout"`
	HandlerWorkers     int           `json:"handler_workers" yaml:"handler_workers"`
	ScanLimit          int           `json:"scan_limit" yaml:"scan_limit"`
}

// Validate 拒绝无上限、无超时或无界扫描/worker配置，不代填业务预算。
func (p WriteBehindFailurePolicy) Validate() error {
	if p.StoreID == "" || len(p.StoreID) > 1024 || p.MaxAttempts < 0 || p.MaxElapsed < 0 || (p.MaxAttempts == 0 && p.MaxElapsed == 0) || p.MinRetryInterval <= 0 || p.MaxRetryInterval < p.MinRetryInterval || p.HandlerMaxAttempts <= 0 || p.HandlerTimeout <= 0 || p.HandlerWorkers < 1 || p.HandlerWorkers > 64 || p.ScanLimit < 1 || p.ScanLimit > 65536 {
		return ErrWriteBehindDisposition
	}
	return nil
}

// WriteBehindFailureState 区分技术投递、结果未知、暂停和已交接；交接不是业务APPLIED。
type WriteBehindFailureState string

const (
	WriteBehindReady      WriteBehindFailureState = "ready"
	WriteBehindInFlight   WriteBehindFailureState = "in_flight"
	WriteBehindUnknown    WriteBehindFailureState = "unknown"
	WriteBehindHandling   WriteBehindFailureState = "handling"
	WriteBehindRetrying   WriteBehindFailureState = "retrying"
	WriteBehindPaused     WriteBehindFailureState = "paused"
	WriteBehindHandedOff  WriteBehindFailureState = "handed_off"
	WriteBehindCompleted  WriteBehindFailureState = "completed"
	WriteBehindReleased   WriteBehindFailureState = "released"
	WriteBehindSuperseded WriteBehindFailureState = "superseded"
)

// WriteBehindFailureAction 仅表达应用授权的处置，Core不按错误文本猜分类。
type WriteBehindFailureAction string

const (
	WriteBehindRetry   WriteBehindFailureAction = "retry"
	WriteBehindResolve WriteBehindFailureAction = "resolve"
	WriteBehindPause   WriteBehindFailureAction = "pause"
	WriteBehindHandoff WriteBehindFailureAction = "handoff"
	WriteBehindConfirm WriteBehindFailureAction = "confirm"
	WriteBehindRelease WriteBehindFailureAction = "release"
)

// WriteBehindFailureScope 表示应用声明的影响范围；已声明顺序组始终不越过未解决前序。
type WriteBehindFailureScope string

const (
	WriteBehindScopeKey    WriteBehindFailureScope = "key"
	WriteBehindScopeGroup  WriteBehindFailureScope = "group"
	WriteBehindScopeTarget WriteBehindFailureScope = "target"
)

// WriteBehindOutcome 是应用对原事务或顺序屏障的明确确认，不由Core从超时/not found推断。
type WriteBehindOutcome string

const (
	WriteBehindNotCommitted     WriteBehindOutcome = "not_committed"
	WriteBehindCommitted        WriteBehindOutcome = "committed"
	WriteBehindStillUnknown     WriteBehindOutcome = "unknown"
	WriteBehindOrderingReleased WriteBehindOutcome = "ordering_released"
)

// WriteBehindHandoffReceipt 是应用已幂等持久提交的回执，Core只校验绑定信息，不能证明任意外部数据库提交。
// DurableReference必须指向应用独立保全的原件/完整引用，不能仅指向Core即将结束的pending。
type WriteBehindHandoffReceipt struct {
	DispositionID    string
	Version          time.Time
	Fingerprint      string
	ReceiptID        string
	Owner            string
	DurableReference string
}

// WriteBehindDecision 绑定原版本和指纹；旧决策不能确认新版本，非法/缺失决策暂停且保留原件。
type WriteBehindDecision struct {
	ID          string
	Version     time.Time
	Fingerprint string
	Action      WriteBehindFailureAction
	Scope       WriteBehindFailureScope
	Outcome     WriteBehindOutcome
	RetryAfter  time.Duration
	Receipt     *WriteBehindHandoffReceipt
}

// WriteBehindFailureRecord 保存可重启的原始快照与尝试状态；仅供应用受权限保护的本地查询，禁止dump到日志。
type WriteBehindFailureRecord[T types.IModel] struct {
	ID              string
	BatchID         string
	LocalConfirmed  bool
	Store           string
	Fingerprint     string
	Snapshot        *SyncQueueItem[T]
	Group           string
	Sequence        uint64
	PreviousID      string
	State           WriteBehindFailureState
	Scope           WriteBehindFailureScope
	Attempts        int
	HandlerAttempts int
	FirstAttemptAt  time.Time
	FirstFailureAt  time.Time
	NextAttemptAt   time.Time
	HandlerDeadline time.Time
	Reason          string
	ErrorMessage    string
	Receipt         *WriteBehindHandoffReceipt
}

// WriteBehindFailure 是一次有界失败快照；当前进程可保留原Cause链，重启后Cause仅恢复持久错误说明。
// 同一原子事务的未确认项可共同声明target范围，不要求应用编造逐条坏数据分类。
type WriteBehindFailure[T types.IModel] struct {
	Record          WriteBehindFailureRecord[T]
	Cause           error
	BudgetExhausted bool
	Waited          time.Duration
	BatchID         string
	Unconfirmed     []WriteBehindFailureRecord[T]
	ConfirmedKeys   []string
}

// WriteBehindFailureHandler 由应用分类、确认未知结果和持久接管；超时不证明原业务或远端事务已停止。
// OrderingGroup必须是无I/O、确定且有界的纯函数，在本地写事务中调用；空组表示仅同key有依赖。
// HandleFailure在固定worker、独立超时上下文和syncExecMu外调用；忽略取消的回调会占用原worker，绝不增补goroutine。
type WriteBehindFailureHandler[T types.IModel] interface {
	OrderingGroup(*SyncQueueItem[T]) (string, error)
	HandleFailure(context.Context, WriteBehindFailure[T]) (WriteBehindDecision, error)
}

// WriteBehindDispositionMetrics 是持久状态计数；技术完成、交接和业务完成不能相加为业务成功数。
type WriteBehindDispositionMetrics struct {
	Ready, Retrying, Unknown, Paused, HandedOff int
	BudgetExhausted                             int
	HandlerActive, HandlerQueued, HandlerLimit  int
	OldestWaitingAt                             time.Time
}
