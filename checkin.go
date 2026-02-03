package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// checkInScheduler 签到调度器
type checkInScheduler struct {
	processor     *MessageProcessor
	lastExecuted  map[string]time.Time // 记录每个任务的上次执行时间，防止重复执行
	lastExecutedMu sync.RWMutex
}

// StartCheckInScheduler 启动定时签到调度器
func (p *MessageProcessor) StartCheckInScheduler(ctx context.Context) error {
	if !p.config.CheckIn.Enabled {
		p.ext.Log().Info("定时签到功能未启用")
		return nil
	}

	if len(p.config.CheckIn.Tasks) == 0 {
		p.ext.Log().Info("没有配置签到任务")
		return nil
	}

	scheduler := &checkInScheduler{
		processor:    p,
		lastExecuted: make(map[string]time.Time),
	}

	p.ext.Log().Info("🕐 定时签到调度器已启动",
		zap.Int("任务数量", len(p.config.CheckIn.Tasks)))

	// 打印所有任务配置
	for i, task := range p.config.CheckIn.Tasks {
		p.ext.Log().Info("📋 签到任务配置",
			zap.Int("任务序号", i+1),
			zap.Int64("机器人ID", task.Bot),
			zap.String("消息内容", task.Message),
			zap.String("Cron表达式", task.Cron))
	}

	// 启动调度循环
	go scheduler.scheduleLoop(ctx)

	// 保持运行
	<-ctx.Done()
	p.ext.Log().Info("定时签到调度器已停止")
	return nil
}

// scheduleLoop 签到调度循环
func (s *checkInScheduler) scheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
	defer ticker.Stop()

	s.processor.ext.Log().Info("⏰ 签到调度循环已启动，每分钟检查一次")

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.checkAndExecuteTasks(ctx, now)
		}
	}
}

// checkAndExecuteTasks 检查并执行需要执行的任务
func (s *checkInScheduler) checkAndExecuteTasks(ctx context.Context, now time.Time) {
	for i, task := range s.processor.config.CheckIn.Tasks {
		taskKey := fmt.Sprintf("task_%d_%d", i, task.Bot)

		// 检查是否应该执行
		if !s.shouldExecute(task, now, taskKey) {
			continue
		}

		// 记录执行时间
		s.lastExecutedMu.Lock()
		s.lastExecuted[taskKey] = now
		s.lastExecutedMu.Unlock()

		// 执行签到
		go func(t CheckInTask, idx int) {
			if err := s.performCheckIn(ctx, t); err != nil {
				s.processor.ext.Log().Error("❌ 签到失败",
					zap.Int("任务序号", idx+1),
					zap.Int64("机器人ID", t.Bot),
					zap.Error(err))
			} else {
				s.processor.ext.Log().Info("✅ 签到成功",
					zap.Int("任务序号", idx+1),
					zap.Int64("机器人ID", t.Bot),
					zap.String("消息", t.Message),
					zap.Time("执行时间", time.Now()))
			}
		}(task, i)
	}
}

// shouldExecute 检查任务是否应该执行
func (s *checkInScheduler) shouldExecute(task CheckInTask, now time.Time, taskKey string) bool {
	// 检查cron表达式是否匹配当前时间
	if !parseCron(task.Cron, now) {
		return false
	}

	// 检查是否已在当前分钟内执行过
	s.lastExecutedMu.RLock()
	lastExec, exists := s.lastExecuted[taskKey]
	s.lastExecutedMu.RUnlock()

	if exists {
		// 如果上次执行在同一分钟内，则不重复执行
		if lastExec.Year() == now.Year() &&
			lastExec.Month() == now.Month() &&
			lastExec.Day() == now.Day() &&
			lastExec.Hour() == now.Hour() &&
			lastExec.Minute() == now.Minute() {
			return false
		}
	}

	return true
}

// parseCron 解析cron表达式并判断是否匹配当前时间
// 支持标准5段式cron: 分钟 小时 日 月 周
// 支持: 数字、*、*/n
func parseCron(cronExpr string, now time.Time) bool {
	parts := strings.Fields(cronExpr)
	if len(parts) != 5 {
		return false
	}

	minute := parts[0]
	hour := parts[1]
	day := parts[2]
	month := parts[3]
	weekday := parts[4]

	// 检查分钟
	if !matchCronField(minute, now.Minute(), 0, 59) {
		return false
	}

	// 检查小时
	if !matchCronField(hour, now.Hour(), 0, 23) {
		return false
	}

	// 检查日期
	if !matchCronField(day, now.Day(), 1, 31) {
		return false
	}

	// 检查月份
	if !matchCronField(month, int(now.Month()), 1, 12) {
		return false
	}

	// 检查星期 (0=Sunday)
	nowWeekday := int(now.Weekday())
	if !matchCronField(weekday, nowWeekday, 0, 6) {
		return false
	}

	return true
}

// matchCronField 匹配单个cron字段
func matchCronField(field string, value int, min int, max int) bool {
	// * 匹配所有
	if field == "*" {
		return true
	}

	// */n 每n个单位
	if strings.HasPrefix(field, "*/") {
		stepStr := strings.TrimPrefix(field, "*/")
		step, err := strconv.Atoi(stepStr)
		if err != nil || step <= 0 {
			return false
		}
		return value%step == 0
	}

	// 逗号分隔的多个值
	if strings.Contains(field, ",") {
		values := strings.Split(field, ",")
		for _, v := range values {
			if num, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				if num == value {
					return true
				}
			}
		}
		return false
	}

	// 范围 n-m
	if strings.Contains(field, "-") {
		rangeParts := strings.Split(field, "-")
		if len(rangeParts) == 2 {
			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 == nil && err2 == nil {
				return value >= start && value <= end
			}
		}
		return false
	}

	// 精确数字
	num, err := strconv.Atoi(field)
	if err != nil {
		return false
	}
	return num == value
}

// performCheckIn 执行签到
func (s *checkInScheduler) performCheckIn(ctx context.Context, task CheckInTask) error {
	// 获取 bot 的 AccessHash
	accessHash, err := s.getBotAccessHash(ctx, task.Bot)
	if err != nil {
		return fmt.Errorf("获取机器人AccessHash失败: %w", err)
	}

	// 创建 InputPeer
	peer := &tg.InputPeerUser{
		UserID:     task.Bot,
		AccessHash: accessHash,
	}

	// 发送消息
	_, err = s.processor.api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:    peer,
		Message: task.Message,
		RandomID: time.Now().UnixNano(),
	})

	if err != nil {
		return fmt.Errorf("发送签到消息失败: %w", err)
	}

	return nil
}

// getBotAccessHash 从对话列表获取 bot 的 AccessHash
func (s *checkInScheduler) getBotAccessHash(ctx context.Context, userID int64) (int64, error) {
	// 获取对话列表
	dialogs, err := s.processor.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetDate: 0,
		OffsetID:   0,
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
		Hash:       0,
	})

	if err != nil {
		return 0, fmt.Errorf("获取对话列表失败: %w", err)
	}

	// 查找对应的用户
	switch d := dialogs.(type) {
	case *tg.MessagesDialogs:
		for _, user := range d.Users {
			if u, ok := user.(*tg.User); ok && u.ID == userID {
				return u.AccessHash, nil
			}
		}
	case *tg.MessagesDialogsSlice:
		for _, user := range d.Users {
			if u, ok := user.(*tg.User); ok && u.ID == userID {
				return u.AccessHash, nil
			}
		}
	}

	return 0, fmt.Errorf("未找到用户 %d，请确保已与该机器人建立过对话", userID)
}
