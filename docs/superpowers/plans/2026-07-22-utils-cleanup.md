# `pkg/utils` 整理实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. 仓库 `AGENTS.md` 要求任务在主线程顺序执行，不派发子代理。

**Goal:** 在不改变 `pkg/utils` 包路径的前提下重排文件、修复低风险缺陷、删除旧 EventBus，并形成高风险 API 审计证据。

**Architecture:** 所有导出辅助函数继续位于 `package utils`。行为修复先写回归测试，再用最小实现通过；纯文件重排保留函数体和签名；影响历史数据或跨节点行为的缺陷只登记，不在本轮改变。

**Tech Stack:** Go 1.26、标准库 `context`/`reflect`/`unicode`、go-zero `logx`、Go race detector、仓库 release-contract。

---

## 文件映射

实现完成后，`pkg/utils` 按以下职责组织：

| 文件 | 职责 |
| --- | --- |
| `concurrency.go` | 有界并发、取消、保序结果和 panic 转 error |
| `json.go` | JSON 序列化兼容辅助 |
| `hash.go` | MD5、SHA-256、用户标识和组合哈希 |
| `string.go` | 首字符大小写和 rune 辅助 |
| `validation.go` | 邮箱和手机号文本校验 |
| `number.go` | 数字文本判断 |
| `random.go` | 旧随机数兼容入口 |
| `time.go` | 旧时间戳转换入口 |
| `runtime.go` | 测试运行判断 |
| `filesystem.go` | 运行路径、文件与目录操作 |
| `network.go` | IP、可信代理和端口探测 |
| `aes.go` | AES-GCM |
| `key_derivation.go` | PBKDF2、盐值和 JWT key |
| `legacy_crypto.go` | DES、3DES 和旧 padding API |
| `decimal.go` | `Decimal` |
| `reflection.go` | 类型、字段、遍历和实例创建 |
| `automap.go` | `AutoMapConvert` 和字段映射缓存 |
| `conversion.go` | 值转换、运算、字节与字符串转换 |
| `snowflake.go` | Snowflake worker 构造 |

删除 `common.go`、`crypto.go`、`file.go`、`ip.go`、`nubmer.go`、`snowflakeid.go`、`types.go` 和 `eventbus.go`。对应实现移动到上表文件，只有本计划明确列出的函数允许改变行为。

### Task 1: 建立可复现基线

**Files:**

- Read: `pkg/utils/*.go`
- Read: `docs/superpowers/specs/2026-07-22-utils-cleanup-design.md`
- Create temporarily: `/private/tmp/core-utils-symbols.before`

- [ ] **Step 1: 确认工作区和并发提交**

Run:

```bash
rtk git status --short
rtk git log -3 --oneline
rtk git show --stat --oneline 0196bc1
```

Expected: `0196bc1` 只包含 `IsPtr(nil)` 防护；没有未识别的 `pkg/utils` 修改。若有新修改，先读取差异并从本计划提交中排除。

- [ ] **Step 2: 保存导出符号基线**

Run:

```bash
rtk proxy go doc -all ./pkg/utils > /private/tmp/core-utils-symbols.before
rtk rg -n '^func |^type |^var |^const ' pkg/utils --glob '*.go'
```

Expected: 基线包含现有导出符号，包括待删除的 `Publisher` 和 `NewPublisher`。

- [ ] **Step 3: 运行现有测试和 race 基线**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -count=1
```

Expected: 两条命令均 PASS。若基线失败，先记录原始失败，不进入文件重排。

### Task 2: 统一 `ConcurrencyTasks` 的 panic、取消和并发上限

**Files:**

- Modify: `pkg/utils/concurrency_test.go`
- Modify: `pkg/utils/concurrency.go`

- [ ] **Step 1: 写串行 panic、预取消和并发上限测试**

在 `concurrency_test.go` 增加以下测试。保留已有 panic 和槽位释放测试：

```go
func TestConcurrencyTasksRunRecoversPanicInSerialMode(t *testing.T) {
	tasks := &ConcurrencyTasks[int]{
		Params:      []int{1},
		Concurrency: 1,
		Func: func(int) (interface{}, error) {
			panic("serial boom")
		},
	}

	tasks.Run()

	if err := tasks.GetErr(); err == nil || !strings.Contains(err.Error(), "serial boom") {
		t.Fatalf("串行 panic 应写入结果，实际 error=%v", err)
	}
}

func TestConcurrencyTasksRunSkipsWorkWhenContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	tasks := &ConcurrencyTasks[int]{
		Ctx:    ctx,
		Params: []int{1, 2, 3},
		Func: func(int) (interface{}, error) {
			calls.Add(1)
			return nil, nil
		},
	}

	tasks.Run()

	if calls.Load() != 0 {
		t.Fatalf("已取消 context 不应执行任务，实际 calls=%d", calls.Load())
	}
	for i, result := range tasks.Results {
		if !errors.Is(result.(error), context.Canceled) {
			t.Fatalf("Results[%d]=%v, want context.Canceled", i, result)
		}
	}
}

func TestConcurrencyTasksRunHonorsConcurrencyLimit(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	tasks := &ConcurrencyTasks[int]{
		Params:      []int{1, 2, 3, 4, 5, 6},
		Concurrency: 2,
		Func: func(param int) (interface{}, error) {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			return param, nil
		},
	}

	tasks.Run()

	if maximum.Load() > 2 {
		t.Fatalf("最大并发=%d, want <=2", maximum.Load())
	}
}
```

同时增加 `context` import。

- [ ] **Step 2: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -run 'TestConcurrencyTasksRun(RecoversPanicInSerialMode|SkipsWorkWhenContextAlreadyCanceled|HonorsConcurrencyLimit)$' -count=1
```

Expected: 串行 panic 测试因 panic 逃逸而 FAIL，预取消测试因 `Func` 被调用而 FAIL。

- [ ] **Step 3: 用固定 worker 和共享执行函数实现 GREEN**

将 `Run`、`extFun` 和 `doFun` 收敛为以下结构。保留 `Successes` 和 `GetErr` 的公共签名：

```go
func (t *ConcurrencyTasks[T]) Run() {
	t.Results = make([]interface{}, len(t.Params))
	if len(t.Params) == 0 {
		return
	}
	ctx := t.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		for index := range t.Results {
			t.Results[index] = err
		}
		return
	}
	workerCount := t.Concurrency
	if workerCount <= 0 {
		workerCount = gConcurrencyCount
	}
	if workerCount > len(t.Params) {
		workerCount = len(t.Params)
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				t.execute(index)
			}
		}()
	}

	next := 0
schedule:
	for ; next < len(t.Params); next++ {
		select {
		case jobs <- next:
		case <-ctx.Done():
			break schedule
		}
	}
	close(jobs)
	workers.Wait()
	for ; next < len(t.Params); next++ {
		t.Results[next] = ctx.Err()
	}
}

func (t *ConcurrencyTasks[T]) execute(index int) {
	param := t.Params[index]
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("panic: %v", recovered)
			t.Results[index] = err
			logx.Errorf("[PANIC]param=%v,err=%v", param, err)
		}
	}()
	result, err := t.Func(param)
	if err != nil {
		t.Results[index] = err
		return
	}
	t.Results[index] = result
}
```

删除字段 `ch`、`wg` 以及私有方法 `extFun`、`doFun`。为文件、导出类型和导出方法补充中文注释。

- [ ] **Step 4: 运行定向测试和 race 确认 GREEN**

Run:

```bash
rtk gofmt -w pkg/utils/concurrency.go pkg/utils/concurrency_test.go
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -run '^TestConcurrencyTasks' -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -run '^TestConcurrencyTasks' -count=1
```

Expected: 全部 `ConcurrencyTasks` 测试 PASS，race detector 无报告。

- [ ] **Step 5: 提交并发修复**

```bash
rtk git add pkg/utils/concurrency.go pkg/utils/concurrency_test.go
rtk git commit -m 'fix(utils): unify bounded task execution'
```

### Task 3: 修复 Unicode 和字节字符串别名

**Files:**

- Create: `pkg/utils/string_test.go`
- Modify: `pkg/utils/types_test.go`
- Modify temporarily: `pkg/utils/common.go`
- Modify temporarily: `pkg/utils/types.go`

- [ ] **Step 1: 写 Unicode 与副本语义测试**

创建 `string_test.go`：

```go
// 本文件验证字符串辅助函数不会切断 UTF-8 字符。
package utils

import "testing"

func TestFirstUpperHandlesUnicodeRune(t *testing.T) {
	if got := FirstUpper("éclair"); got != "Éclair" {
		t.Fatalf("FirstUpper()=%q, want %q", got, "Éclair")
	}
}

func TestFirstLowerHandlesUnicodeRune(t *testing.T) {
	if got := FirstLower("Éclair"); got != "éclair" {
		t.Fatalf("FirstLower()=%q, want %q", got, "éclair")
	}
}
```

在 `types_test.go` 增加：

```go
func TestBytes2StringReturnsIndependentString(t *testing.T) {
	input := []byte("abc")
	got := Bytes2String(input)
	input[0] = 'z'
	if got != "abc" {
		t.Fatalf("Bytes2String() 与输入共享内存，实际=%q", got)
	}
}

func TestString2BytesReturnsIndependentBytes(t *testing.T) {
	input := "abc"
	got := String2Bytes(input)
	got[0] = 'z'
	if input != "abc" {
		t.Fatalf("String2Bytes() 修改了输入字符串，实际=%q", input)
	}
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -run 'Test(FirstUpperHandlesUnicodeRune|FirstLowerHandlesUnicodeRune|Bytes2StringReturnsIndependentString|String2BytesReturnsIndependentBytes)$' -count=1
```

Expected: Unicode 测试和 `Bytes2String` 独立副本测试 FAIL。

- [ ] **Step 3: 写最小安全实现**

将首字符转换改为：

```go
func FirstUpper(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(first)) + s[size:]
}

func FirstLower(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToLower(first)) + s[size:]
}
```

将转换函数改为：

```go
func String2Bytes(s string) []byte { return []byte(s) }

func Bytes2String(b []byte) string { return string(b) }
```

删除 `types.go` 的 `unsafe` import，并在 `common.go` 增加 `unicode` import。

- [ ] **Step 4: 运行测试确认 GREEN**

Run:

```bash
rtk gofmt -w pkg/utils/common.go pkg/utils/string_test.go pkg/utils/types.go pkg/utils/types_test.go
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -count=1
```

Expected: 全部 PASS；`rtk rg -n 'unsafe' pkg/utils --glob '*.go'` 无结果。

- [ ] **Step 5: 提交字符串安全修复**

```bash
rtk git add pkg/utils/common.go pkg/utils/string_test.go pkg/utils/types.go pkg/utils/types_test.go
rtk git commit -m 'fix(utils): make string helpers unicode safe'
```

### Task 4: 移除反射缓存的隐式进程生命周期

**Files:**

- Create: `pkg/utils/lifecycle_test.go`
- Modify: `pkg/utils/types.go`
- Modify: `docs/codex/DEPRECATION_REGISTER.md`

- [ ] **Step 1: 写禁止包级反射监控的静态契约测试**

创建 `lifecycle_test.go`，扫描当前包生产文件：

```go
// 本文件约束 utils 不通过 init 接管整进程的内存和 GC 生命周期。
package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageDoesNotOwnReflectionMemoryMonitor(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		for _, forbidden := range []string{"startReflectionMemoryMonitor", "runtime.GC()"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s 仍包含无 owner 的生命周期逻辑 %q", file, forbidden)
			}
		}
	}
}
```

在 `types_test.go` 增加 nil 边界测试：

```go
func TestReflectionHelpersHandleNilInputs(t *testing.T) {
	if NewInterface(nil) != nil || NewInterfaceByType(nil) != nil {
		t.Fatal("nil 类型不应创建实例")
	}
	if GetTypeName(nil) != "" || GetTypeKind(nil) != Invalid || GetElem(nil) != nil {
		t.Fatal("nil 类型应返回稳定零值")
	}
	typ, value := GetTypeAndValue(nil)
	if typ != nil || value.IsValid() {
		t.Fatalf("GetTypeAndValue(nil)=(%v, %v), want nil and invalid value", typ, value)
	}
	if HasProperty(nil, "ID") || GetPropertyType(nil, "ID") != nil ||
		GetPropertyTypeByElemName(nil, "Item") != nil {
		t.Fatal("nil 目标不应报告任何字段")
	}
	if got := GetPropertyValue(nil, "ID"); got != "" {
		t.Fatalf("GetPropertyValue(nil)=%v, want empty string", got)
	}
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -run 'Test(PackageDoesNotOwnReflectionMemoryMonitor|ReflectionHelpersHandleNilInputs)$' -count=1
```

Expected: FAIL。生命周期测试指出 `types.go` 包含 `startReflectionMemoryMonitor`，nil 测试在 `GetTypeName(nil)` 等入口触发 panic。

- [ ] **Step 3: 删除隐式 monitor，保留兼容入口**

删除 `memMonitorStop`、`init`、`startReflectionMemoryMonitor` 和 `cleanReflectionCaches`。删除不再使用的 `runtime`、`time` 和 `logx` import。保留：

```go
// StopMemoryMonitor 为旧反射缓存监控的兼容入口。
// Deprecated: utils 不再启动包级内存监控，调用该函数不会执行操作。
func StopMemoryMonitor() {}
```

为反射入口增加一致的 nil guard。每个 guard 放在函数第一行：

| 函数 | guard 返回值 |
| --- | --- |
| `NewInterface` | `obj == nil` 时返回 `nil` |
| `NewInterfaceByType` | `typ == nil` 时返回 `nil` |
| `GetTypeName` | `item == nil` 时返回 `""` |
| `GetTypeKind` | `typ == nil` 时返回 `Invalid` |
| `GetTypeAndValue` | `target == nil` 时返回 `nil, reflect.Value{}` |
| `getType`、`GetElem` | `typ == nil` 时返回 `nil` |
| `HasProperty` | `target == nil` 时返回 `false` |
| `GetPropertyType`、`GetPropertyTypeByElemName` | `target == nil` 时返回 `nil` |
| `GetPropertyValue` | `target == nil` 时返回 `""` |

guard 只处理 nil。不要改变字段缺失或不可设置时的旧 no-op 语义。

在 `DEPRECATION_REGISTER.md` 增加一行：首次登记版本使用 `v0.0.251`，最早删除版本为 `v0.1.0`，替代入口写“由资源 owner 管理指标和内存策略”。

- [ ] **Step 4: 补齐 `IsPtr(nil)` 回归覆盖**

`0196bc1` 已先于本计划加入实现，不能伪造 RED。只增加以下回归测试，确认既有修复受保护：

```go
func TestIsPtrReturnsFalseForNilInterface(t *testing.T) {
	if IsPtr(nil) {
		t.Fatal("nil interface 不应被识别为指针")
	}
}
```

- [ ] **Step 5: 运行生命周期和反射测试确认 GREEN**

Run:

```bash
rtk gofmt -w pkg/utils/types.go pkg/utils/types_test.go pkg/utils/lifecycle_test.go
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -count=1
```

Expected: 全部 PASS，无后台 monitor 禁止项。

- [ ] **Step 6: 提交生命周期修复**

```bash
rtk git add pkg/utils/types.go pkg/utils/types_test.go pkg/utils/lifecycle_test.go docs/codex/DEPRECATION_REGISTER.md
rtk git commit -m 'fix(utils): remove implicit memory monitor'
```

### Task 5: 按职责重排源文件

**Files:**

- Create: `pkg/utils/{json,hash,string,validation,number,random,time,runtime,filesystem,network,aes,key_derivation,legacy_crypto,decimal,reflection,automap,conversion,snowflake}.go`
- Delete: `pkg/utils/{common,crypto,file,ip,nubmer,snowflakeid,types}.go`

- [ ] **Step 1: 先移动只需改名的文件**

Run:

```bash
rtk git mv pkg/utils/file.go pkg/utils/filesystem.go
rtk git mv pkg/utils/ip.go pkg/utils/network.go
rtk git mv pkg/utils/nubmer.go pkg/utils/number.go
rtk git mv pkg/utils/snowflakeid.go pkg/utils/snowflake.go
```

为四个文件增加中文文件级注释，不改函数体。

- [ ] **Step 2: 拆分 `common.go`**

按以下符号移动，函数体除 Task 3 已确认修改外保持不变：

```text
json.go: PrintObj
hash.go: Md5, UserIDHash, UserIDUUID, ShortHash, MediumHash,
         SecureHash, HashCodeHex, HashCode64, HashCodes
string.go: FirstUpper, FirstLower, TrimFirstRune
validation.go: IsEmail, IsMobile
random.go: GetRandNum
time.go: ToTime
runtime.go: IsTest
```

删除空的 `common.go`。

- [ ] **Step 3: 拆分 `crypto.go`**

按以下符号原样移动：

```text
aes.go: EncryptAES, DecryptAES
key_derivation.go: DeriveKey, GenerateSalt, DeriveKeySecure,
                   DeriveKeyWithSalt, DeriveJWTKey
legacy_crypto.go: PaddingText, UnPaddingText, EncyptogDES, DecrptogDES,
                  Encyptog3DES, Decrptog3DES
```

删除空的 `crypto.go`。`legacy_crypto.go` 文件级注释明确它只为兼容保留，不推荐新代码使用。

- [ ] **Step 4: 拆分 `types.go`**

按以下连续能力移动，保留私有 helper 与调用者同文件：

```text
decimal.go: Decimal 及其方法
reflection.go: AutoMapArge, AutoMapHander, TypeKind, NewInterface,
               NewInterfaceByType, RecycleObject, StopMemoryMonitor,
               GetPackageName 至 NewArrayItem 的反射类型、字段和遍历函数
automap.go: fieldMappingCache, fieldMapping, getCacheKey,
            autoMapConvertList, AutoMapConvert, buildFieldMappings,
            convertAndSet
conversion.go: AnyToTypeData 至 IsEqual，包含 convertString、convertOp1、
               Add、String2Bytes 和 Bytes2String
```

若 `AutoMapArge` 和 `AutoMapHander` 只被 automap 使用，则移入 `automap.go`。删除空的 `types.go`。

- [ ] **Step 5: 补齐注释并格式化**

每个文件顶部增加中文文件级注释。每个导出类型、函数、方法和变量增加以标识符开头的中文 Go doc。执行：

```bash
rtk gofmt -w pkg/utils/*.go
rtk gofmt -d pkg/utils/*.go
```

Expected: 第二条命令无输出。

- [ ] **Step 6: 验证文件重排未改变行为**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./service/manage ./pkg/persistence/types -count=1
```

Expected: 全部 PASS。

- [ ] **Step 7: 提交文件重排**

```bash
rtk git add pkg/utils
rtk git commit -m 'refactor(utils): organize helpers by responsibility'
```

### Task 6: 删除旧 EventBus 并登记迁移

**Files:**

- Delete: `pkg/utils/eventbus.go`
- Modify: `CHANGELOG.md`
- Modify: `docs/codex/DEAD_CODE_AUDIT.md`

- [ ] **Step 1: 再次证明仓库内零调用**

Run:

```bash
rtk rg -n '\b(NewPublisher|Publisher|SubscribeTopic|\.Evict\()\b' . --glob '*.go' --glob '*.md'
```

Expected: `pkg/utils/eventbus.go` 自身定义，以及 `pkg/server/types/observable.go` 中无关的同名接口；没有 `utils.NewPublisher` 调用。若出现真实调用，停止本任务。

- [ ] **Step 2: 删除旧实现并写迁移记录**

Run:

```bash
rtk git rm pkg/utils/eventbus.go
```

在 `CHANGELOG.md` 的 Unreleased/Removed 段增加：

```markdown
- 删除未使用的实验性 `utils.Publisher`。进程内事件改用 `pkg/server/event.Stream`，服务事件改用 `ServiceContext` 管理的 EventBridge。
```

在 `DEAD_CODE_AUDIT.md` 清理台账增加 `pkg/utils/eventbus.go`，证据写明“全仓零调用、2022 年后无演进、现行替代为 server/event”，分类为 `remove-experimental`。

- [ ] **Step 3: 验证删除结果**

Run:

```bash
rtk rg -n 'utils\.(NewPublisher|Publisher)|SubscribeTopic' . --glob '*.go' --glob '*.md'
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils ./pkg/server/event -count=1
```

Expected: 扫描无结果；测试 PASS。

- [ ] **Step 4: 提交删除**

```bash
rtk git add CHANGELOG.md docs/codex/DEAD_CODE_AUDIT.md pkg/utils/eventbus.go
rtk git commit -m 'refactor(utils): remove obsolete event bus'
```

### Task 7: 形成高风险 API 审计记录

**Files:**

- Create: `docs/codex/UTILS_RISK_AUDIT.md`

- [ ] **Step 1: 运行调用面和失败样例检查**

Run:

```bash
rtk rg -n '\b(EncyptogDES|DecrptogDES|Encyptog3DES|Decrptog3DES|HashCodes|NewAlgorithmSnowFlake|ToTime|PrintObj|GetRandNum)\b' . --glob '*.go' --glob '*.md'
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
```

记录每个符号的框架调用、示例调用和零调用状态。

- [ ] **Step 2: 写审计表**

文档至少包含以下列：

```markdown
| API | 当前风险 | 调用证据 | 兼容影响 | 本轮决策 | 后续建议 |
| --- | --- | --- | --- | --- | --- |
| DES/3DES | 无效 key 和损坏 padding 可能 panic | 全仓扫描结果 | 改签名或密文会破坏调用方 | 保留行为 | 新增返回 error 的版本化 API 后废弃旧入口 |
| `HashCodes` | 分隔拼接存在输入组合碰撞 | 模型和缓存 key 调用 | 改算法影响已落库 hash | 保留行为 | 设计带长度前缀的 `HashCodesV2` 和迁移窗口 |
| `NewAlgorithmSnowFlake` | 十进制拼接再转 `uint16` 可能碰撞或截断 | Router、Model 和 07 示例 | 改规则影响分布式 ID | 保留行为 | 单独定义 MachineID/DataCenterID 位宽契约 |
| `ToTime`/`PrintObj` | 错误被丢弃 | 调用扫描结果 | 改签名破坏源码 | 保留行为 | 新增显式 error API |
| `GetRandNum` | 全局 Seed 和非法上限 panic | 调用扫描结果 | 改随机序列 | 保留行为 | 新增接收 `rand.Source` 或返回 error 的 API |
```

不得写“已修复”，因为本任务只记录这些高风险项。

- [ ] **Step 3: 提交审计文档**

```bash
rtk git add docs/codex/UTILS_RISK_AUDIT.md
rtk git commit -m 'docs: audit high risk utils APIs'
```

### Task 8: 完整验证和五轴复核

**Files:**

- Verify: all changed files
- Compare: `/private/tmp/core-utils-symbols.before`

- [ ] **Step 1: 运行格式、定向测试和 race**

Run:

```bash
rtk gofmt -d pkg/utils/*.go
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./service/manage ./pkg/persistence/types -count=1
```

Expected: gofmt 无输出，全部测试 PASS，race detector 无报告。

- [ ] **Step 2: 运行全仓编译和发布契约**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./... -run '^$'
rtk proxy ./scripts/test.sh release-contract
```

Expected: 全仓编译 PASS。若 release-contract 因消费方或外部环境失败，记录具体 gate 和原始错误，不把阻塞项报告为通过。

- [ ] **Step 3: 比较导出 API**

Run:

```bash
rtk proxy go doc -all ./pkg/utils > /private/tmp/core-utils-symbols.after
rtk proxy diff -u /private/tmp/core-utils-symbols.before /private/tmp/core-utils-symbols.after
```

Expected: 允许注释变化和 `Publisher` 系列删除；不允许其他导出签名删除或改变。

- [ ] **Step 4: 执行五轴代码复核**

逐文件检查：

1. Correctness：结果顺序、错误、取消、nil、UTF-8 和副本语义符合测试
2. Readability：每个文件职责单一，注释解释边界而非复述语法
3. Architecture：没有新增子包、全局 owner 或第二套事件系统
4. Security：没有 `unsafe` 别名，没有扩大旧密码 API 使用面
5. Performance：worker 数有界，没有包初始化 goroutine 或主动全局 GC

- [ ] **Step 5: 检查提交和工作区**

Run:

```bash
rtk git status --short
rtk git log --oneline -8
rtk git diff 25a44fe..HEAD --check
```

Expected: 工作区干净，提交边界分别覆盖行为修复、结构重排、EventBus 删除和风险审计。
