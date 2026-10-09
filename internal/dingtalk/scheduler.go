package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"

	"github.com/Tencent/WeKnora/internal/logger"
)

// syncCronParser 兼容 5 段（分）与 6 段（秒）表达式——配置默认值
// "0 0 3 * * *" 是 6 段式，datasource.Scheduler 也用了同款带秒的 parser。
var syncCronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Scheduler 钉钉通讯录同步的 cron 调度器。
type Scheduler struct {
	cron *cron.Cron
	sync *SyncService
	expr string
}

// NewScheduler 构造调度器。cronExpr 为 5 段或 6 段 cron 表达式
// （配置层已填默认值 "0 0 3 * * *"，见 config.applyDingTalkEnvOverrides）。
func NewScheduler(sync *SyncService, cronExpr string) *Scheduler {
	return &Scheduler{
		cron: cron.New(cron.WithParser(syncCronParser), cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		)),
		sync: sync,
		expr: strings.TrimSpace(cronExpr),
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
		if _, err := s.sync.RunFullSync(ctx, SyncTriggerCron); err != nil {
			logger.Errorf(ctx, "DingTalk contact sync failed: %v", err)
		}
	}); err != nil {
		return fmt.Errorf("register dingtalk sync cron: %w", err)
	}
	s.cron.Start()
	logger.Infof(context.Background(), "DingTalk contact sync scheduler started (cron=%q)", s.expr)
	return nil
}

// Stop 停止调度并等待进行中的同步任务结束。
func (s *Scheduler) Stop() {
	if s.cron == nil {
		return
	}
	<-s.cron.Stop().Done()
}
