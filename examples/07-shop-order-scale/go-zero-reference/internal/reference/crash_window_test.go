// 本文件用独立子进程真实硬杀SQL提交和发布确认窗口，不增加生产故障注入入口。
package reference

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestNativeCrashCheckpointWorker 只由下方测试子进程调用，真实提交后等待父进程SIGKILL。
func TestNativeCrashCheckpointWorker(t *testing.T) {
	phase := os.Getenv("CORE_REFERENCE_CRASH_WORKER")
	if phase == "" {
		t.Skip("仅由隔离崩溃测试调用")
	}
	db, err := openDatabase("order", "")
	require.NoError(t, err)
	s, err := openPending(os.Getenv("CORE_REFERENCE_CRASH_PATH"))
	require.NoError(t, err)
	id, err := strconv.ParseUint(os.Getenv("CORE_REFERENCE_CRASH_ORDER"), 10, 64)
	require.NoError(t, err)
	now := time.Now().UTC()
	o := testOrder(uint(id), fmt.Sprintf("crash-window-%d", id))
	o.CreatedAt = &now
	o.UpdatedAt = &now
	o.AcceptedAt = now
	_, err = s.accept(context.Background(), o)
	require.NoError(t, err)
	a := &application{db: db, local: s, clock: clock{worker: 3}}
	require.NoError(t, a.syncBatch(context.Background(), []*Order{o}))
	if phase == "after-publish" {
		a.redis = redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
		var items []*OutboxRecord
		require.NoError(t, db.Where("event_id = ?", fmt.Sprintf("order:%d:OrderCreated", id)).Find(&items).Error)
		published, err := a.publishBatch(context.Background(), items)
		require.NoError(t, err)
		require.Len(t, published, 1)
	}
	require.NoError(t, os.WriteFile(os.Getenv("CORE_REFERENCE_CRASH_READY"), []byte(phase), 0600))
	for {
		time.Sleep(time.Second)
	}
}

// TestNativeCrashWindows 验证事务提交/本地确认和发布/标记两个窗口原路径恢复，允许可追踪重投。
func TestNativeCrashWindows(t *testing.T) {
	root := os.Getenv("CORE_REFERENCE_CRASH_EVIDENCE")
	if os.Getenv("CORE_REFERENCE_TEST_MYSQL") != "1" || root == "" || os.Getenv("CORE_TEST_REDIS_ADDR") == "" {
		t.Skip("需要本轮专用MySQL、Redis及保留目录")
	}
	for _, phase := range []string{"after-sql", "after-publish"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(root, phase)
			require.NoError(t, os.Mkdir(path, 0700))
			ready := filepath.Join(path, "ready")
			pendingPath := filepath.Join(path, "pending")
			id := uint(time.Now().UnixNano() >> 8)
			eventID := fmt.Sprintf("order:%d:OrderCreated", id)
			binary, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.Command(binary, "-test.run=^TestNativeCrashCheckpointWorker$", "-test.v")
			cmd.Env = append(os.Environ(), "CORE_REFERENCE_CRASH_WORKER="+phase, "CORE_REFERENCE_CRASH_ORDER="+strconv.FormatUint(uint64(id), 10), "CORE_REFERENCE_CRASH_PATH="+pendingPath, "CORE_REFERENCE_CRASH_READY="+ready)
			log, err := os.OpenFile(filepath.Join(path, "child.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			require.NoError(t, err)
			defer log.Close()
			cmd.Stdout = log
			cmd.Stderr = log
			require.NoError(t, cmd.Start())
			waited := false
			defer func() {
				if !waited {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 30*time.Second, 10*time.Millisecond)
			require.NoError(t, cmd.Process.Kill())
			require.Error(t, cmd.Wait())
			waited = true
			s, err := openPending(pendingPath)
			require.NoError(t, err)
			defer s.close()
			items, err := s.batch(512)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, id, items[0].ID)
			db, err := openDatabase("order", "")
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			defer pool.Close()
			a := &application{db: db, local: s, clock: clock{worker: 3}}
			require.NoError(t, a.syncBatch(context.Background(), items))
			var count int64
			require.NoError(t, db.Model(&Order{}).Where("id = ?", id).Count(&count).Error)
			require.EqualValues(t, 1, count)
			var outboxes []*OutboxRecord
			require.NoError(t, db.Where("event_id = ?", eventID).Find(&outboxes).Error)
			require.Len(t, outboxes, 1)
			require.False(t, outboxes[0].Published)
			if phase == "after-publish" {
				a.redis = redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
				defer a.redis.Close()
				published, err := a.publishBatch(context.Background(), outboxes)
				require.NoError(t, err)
				require.Len(t, published, 1)
				require.NoError(t, a.markPublished(context.Background(), published))
				messages, err := a.redis.XRange(context.Background(), streamName, "-", "+").Result()
				require.NoError(t, err)
				copies := 0
				for _, message := range messages {
					if message.Values["idempotency_key"] == eventID {
						copies++
					}
				}
				require.Equal(t, 2, copies, "发布后/标记前硬杀允许相同EventID重投")
			}
			require.NoError(t, s.confirm(items))
			require.Zero(t, s.depth.Load())
		})
	}
}
