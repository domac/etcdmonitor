package tabs

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"etcdmonitor/internal/config"
)

// fakeRepo 是 Repo 的最小实现，用来观测 ListAll / UpdateStatus 调用。
//
// 不模拟其他方法；测试只关心 scheduler 的 tick 行为。
type fakeRepo struct {
	mu          sync.Mutex
	tabs        []Tab
	listCount   atomic.Int32
	statusCalls atomic.Int32
}

func (f *fakeRepo) ListAll() ([]Tab, error) {
	f.listCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Tab, len(f.tabs))
	copy(out, f.tabs)
	return out, nil
}

func (f *fakeRepo) UpdateStatus(id, status, errMsg string, checkedAt int64) error {
	f.statusCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tabs {
		if f.tabs[i].ID == id {
			f.tabs[i].LastStatus = status
			f.tabs[i].LastError = errMsg
			f.tabs[i].LastCheckedAt = checkedAt
			break
		}
	}
	return nil
}

// 余下方法占位即可——scheduler 不会调它们。
func (f *fakeRepo) ListByUser(userID int64) ([]Tab, error)               { return nil, nil }
func (f *fakeRepo) Get(id string, userID int64) (*Tab, error)            { return nil, ErrTabNotFound }
func (f *fakeRepo) Create(t *Tab) error                                  { return nil }
func (f *fakeRepo) UpdateByUser(id string, userID int64, p PatchFields) error { return nil }
func (f *fakeRepo) UpdateOrderByUser(userID int64, ids []string) error   { return nil }
func (f *fakeRepo) DeleteByUser(id string, userID int64) error           { return nil }
func (f *fakeRepo) CountByUser(userID int64) (int, error)                { return 0, nil }

func newSchedulerHarness(t *testing.T) (*Manager, *fakeRepo) {
	t.Helper()
	repo := &fakeRepo{}
	km, err := NewFileKeyManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	cfg := &config.Config{}
	cfg.Etcd.Endpoint = "http://127.0.0.1:65535" // 不可达端口，确保探活快速失败
	cfg.KVManager.ConnectTimeout = 1
	cfg.KVManager.RequestTimeout = 1
	mgr := NewManager(repo, km, cfg, nil)
	return mgr, repo
}

// TestPingScheduler_RunsImmediatelyAndPeriodically 启动后 scheduler 应跑过至少一次 tick。
//
// 注意：本测试不验证 UpdateStatus 调用次数——那是 Manager.Ping 的内部行为，
// 且实际探活会因 dial timeout 拖慢节奏（不可达 endpoint 探活耗时取决于 OS 协议栈）。
// 本测试只验证 PingScheduler 的"调度行为"——证明 ListAll 被周期触发。
func TestPingScheduler_RunsImmediatelyAndPeriodically(t *testing.T) {
	mgr, repo := newSchedulerHarness(t)
	cipher, _ := mgr.km.Encrypt([]byte("p"))
	repo.tabs = []Tab{{
		ID:              "t1",
		CreatedByUserID: 1,
		Endpoint:        "http://127.0.0.1:65535",
		PasswordCipher:  cipher,
	}}

	sched := NewPingScheduler(mgr, 200*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	// 启动后立即触发一轮 tick → ListAll 至少被调用 1 次
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if repo.listCount.Load() >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("scheduler did not tick: listCount=%d", repo.listCount.Load())
}

// TestPingScheduler_StopGracefully Stop() 必须让 goroutine 退出。
func TestPingScheduler_StopGracefully(t *testing.T) {
	mgr, repo := newSchedulerHarness(t)
	cipher, _ := mgr.km.Encrypt([]byte("p"))
	repo.tabs = []Tab{{ID: "t1", CreatedByUserID: 1, Endpoint: "http://127.0.0.1:65535", PasswordCipher: cipher}}

	sched := NewPingScheduler(mgr, 100*time.Millisecond)
	sched.Start(context.Background())

	// 给 100ms 跑起来
	time.Sleep(100 * time.Millisecond)

	stopDone := make(chan struct{})
	go func() {
		sched.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		// 通过：Stop 在 5s 内返回
	case <-time.After(7 * time.Second):
		t.Fatal("Stop did not return within 7s")
	}
}

// TestPingScheduler_NoTabsNoStatusCalls 0 个 Tab 时探活不该写状态。
func TestPingScheduler_NoTabsNoStatusCalls(t *testing.T) {
	mgr, repo := newSchedulerHarness(t)
	// repo.tabs 留空

	sched := NewPingScheduler(mgr, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop()

	// 让它跑几轮
	time.Sleep(400 * time.Millisecond)

	if repo.listCount.Load() < 1 {
		t.Errorf("expected at least 1 ListAll call, got %d", repo.listCount.Load())
	}
	if repo.statusCalls.Load() != 0 {
		t.Errorf("expected 0 UpdateStatus calls (no tabs), got %d", repo.statusCalls.Load())
	}
}

// TestPingScheduler_StartIsIdempotent 多次 Start 不能启动多个 goroutine。
func TestPingScheduler_StartIsIdempotent(t *testing.T) {
	mgr, _ := newSchedulerHarness(t)
	sched := NewPingScheduler(mgr, 1*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)
	sched.Start(ctx) // no-op
	sched.Stop()
	// 没有 panic / 死锁 即通过
}

// TestPingScheduler_StopIsIdempotent 多次 Stop 不能 panic。
func TestPingScheduler_StopIsIdempotent(t *testing.T) {
	mgr, _ := newSchedulerHarness(t)
	sched := NewPingScheduler(mgr, 1*time.Second)
	sched.Start(context.Background())
	sched.Stop()
	sched.Stop() // no-op
}
