package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Tencent/WeKnora/internal/logger"
)

// syncCronParser 兼容 5 段（分）与 6 段（秒）表达式——配置默认值
// "0 17 3 * * *"（每日 03:17）是 6 段式，datasource.Scheduler 也用了同款带秒的 parser。
var syncCronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

const (
	// syncRetryDelay 单次 cron 同步失败后的补跑间隔。
	syncRetryDelay = 10 * time.Minute
	// maxSyncRetries 单轮计划最多补跑次数（不含首次执行，共 3 次机会）。
	maxSyncRetries = 2
)

// Scheduler 钉钉通讯录同步的 cron 调度器。
type Scheduler struct {
	cron *cron.Cron
	sync *SyncService
	expr string

	// 补跑定时器的生命周期管理：Stop 时要连待补跑一起取消，
	// 否则进程退出前可能凭空多出一次同步。
	mu     sync.Mutex
	timers map[*time.Timer]struct{}
	done   chan struct{}
	once   sync.Once
}

// NewScheduler 构造调度器。cronExpr 为 5 段或 6 段 cron 表达式
// （配置层已填默认值 "0 17 3 * * *"，见 config.applyDingTalkEnvOverrides）。
func NewScheduler(sync *SyncService, cronExpr string) *Scheduler {
	return &Scheduler{
		cron: cron.New(cron.WithParser(syncCronParser), cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		)),
		sync:   sync,
		expr:   strings.TrimSpace(cronExpr),
		timers: make(map[*time.Timer]struct{}),
		done:   make(chan struct{}),
	}
}

// Start 校验表达式、注册同步任务并后台启动 cron runner。
// 表达式非法时返回 error，调度器不启动。
func (s *Scheduler) Start() error {
	if s.sync == nil {
		return errors.New("dingtalk sync scheduler: nil sync service")
	}
	if _, err := syncCronParser.Parse(s.expr); err != nil {
		return fmt.Errorf("invalid dingtalk sync cron %q: %w", s.expr, err)
	}
	if _, err := s.cron.AddFunc(s.expr, func() {
		ctx := context.Background()
		logger.Infof(ctx, "DingTalk contact sync triggered by cron")
		s.runWithRetry(ctx)
	}); err != nil {
		return fmt.Errorf("register dingtalk sync cron: %w", err)
	}
	s.cron.Start()
	logger.Infof(context.Background(), "DingTalk contact sync scheduler started (cron=%q)", s.expr)
	return nil
}

// runWithRetry 执行一轮 cron 同步；失败时隔 10 分钟补跑，最多补跑 2 次。
// ErrSyncAlreadyRunning 属于正常并发保护（手动触发占了位），不补跑。
func (s *Scheduler) runWithRetry(ctx context.Context) {
	const totalAttempts = 1 + maxSyncRetries
	for attempt := 1; ; attempt++ {
		_, err := s.sync.RunFullSync(ctx, SyncTriggerCron)
		if err == nil {
			if attempt > 1 {
				logger.Infof(ctx, "DingTalk contact sync retry succeeded (attempt %d/%d)", attempt, totalAttempts)
			}
			return
		}
		if errors.Is(err, ErrSyncAlreadyRunning) {
			logger.Infof(ctx, "DingTalk contact sync skipped: another sync is running")
			return
		}
		if attempt >= totalAttempts {
			logger.Errorf(ctx, "DingTalk contact sync failed after %d attempts, giving up: %v", attempt, err)
			return
		}
		logger.Errorf(ctx, "DingTalk contact sync failed, retry in %s (attempt %d/%d): %v",
			syncRetryDelay, attempt, totalAttempts, err)
		if !s.waitRetry(syncRetryDelay) {
			return
		}
	}
}

// waitRetry 等待补跑间隔；调度器停止时立即返回 false。
func (s *Scheduler) waitRetry(d time.Duration) bool {
	fired := make(chan struct{})
	timer := time.AfterFunc(d, func() { close(fired) })
	s.mu.Lock()
	s.timers[timer] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.timers, timer)
		s.mu.Unlock()
	}()
	select {
	case <-fired:
		return true
	case <-s.done:
		timer.Stop()
		return false
	}
}

// Stop 停止调度并等待进行中的同步任务结束；待补跑的定时器一并取消。
func (s *Scheduler) Stop() {
	if s.cron == nil {
		return
	}
	<-s.cron.Stop().Done()
	s.once.Do(func() { close(s.done) })
	s.mu.Lock()
	for t := range s.timers {
		t.Stop()
	}
	s.mu.Unlock()
}
