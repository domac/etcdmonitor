package tabs

import (
	"context"
	"sync"
	"time"

	"etcdmonitor/internal/logger"
)

// PingScheduler 在后台周期性地探活所有非默认 Tab。
//
// 单 goroutine 串行扫描——每轮通过 Repo.ListAll() 拿到 DB 中所有 Tab，逐个调用
// Manager.Ping。失败的 Ping 不阻塞下一个 Tab。
//
// 与会话身份正交（D11）：scheduler 不持有 user_id，所有用户的 Tab 都被同等探活。
type PingScheduler struct {
	mgr      *Manager
	interval time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewPingScheduler 构造一个未启动的 PingScheduler。
//
// interval 是探活周期；建议 ≥ 10 秒。配置层 (config.go) 已强制最小值。
func NewPingScheduler(mgr *Manager, interval time.Duration) *PingScheduler {
	return &PingScheduler{
		mgr:      mgr,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Start 在后台 goroutine 中启动周期探活。
//
// 第一轮探活在调用 Start 后立即触发（不等 interval），让"刚启动的进程"能尽快
// 获得新鲜状态。后续按 interval 间隔。
//
// 多次调用 Start 是 no-op（startOnce 守护）；进程级单例。
func (s *PingScheduler) Start(parent context.Context) {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		s.cancel = cancel
		go s.run(ctx)
	})
}

// run 是单 goroutine 主循环。
func (s *PingScheduler) run(ctx context.Context) {
	defer close(s.done)

	logger.Infof("[KV-Tabs] PingScheduler started (interval=%v)", s.interval)

	// 立刻跑一轮
	s.tick(ctx)

	t := time.NewTicker(s.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infof("[KV-Tabs] PingScheduler stopping")
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

// tick 一轮探活：列出所有非默认 Tab，串行 ping。
//
// 每个 Tab 的 ping 内部用 5 秒硬超时（与 TestConnection 一致，见 Manager.Ping）；
// 最坏情况一轮总耗时 = N * 5s（N=非默认 Tab 数）。设置 interval ≥ 60 秒能保证
// 10 个 Tab 都不挤压。
func (s *PingScheduler) tick(ctx context.Context) {
	all, err := s.mgr.repo.ListAll()
	if err != nil {
		logger.Warnf("[KV-Tabs] PingScheduler tick: ListAll failed: %v", err)
		return
	}
	for _, tab := range all {
		if ctx.Err() != nil {
			// 收到关闭信号，立即退出（不写回未完成探活的状态）
			return
		}
		// Ping 内部已经做了 KEK 解密 + UpdateStatus 写回；失败仅记日志
		_, _ = s.mgr.Ping(tab.ID)
	}
}

// Stop 优雅关闭——cancel context 后等 goroutine 退出。
//
// 5 秒超时后强制返回，避免阻塞主进程关闭流程。多次调用是 no-op（stopOnce 守护）。
func (s *PingScheduler) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		select {
		case <-s.done:
			// 优雅退出
		case <-time.After(5 * time.Second):
			logger.Warnf("[KV-Tabs] PingScheduler did not stop within 5s; abandoning")
		}
	})
}
