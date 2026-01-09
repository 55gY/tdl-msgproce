package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// ForwardTask 转发任务
type ForwardTask struct {
	ID            int
	Link          string
	UserID        int64
	Status        string   // pending, running, completed, cancelled, failed
	Progress      int      // 0-100 进度百分比
	ProgressLines []string // 最近的进度输出行（用于调试）
	Error         string
	Cancelled     bool
	CancelMutex   sync.Mutex
	ProgressMutex sync.Mutex
}

// BatchTask 批量任务
type BatchTask struct {
	BatchID   int
	UserID    int64
	Tasks     []*ForwardTask
	StatusMsg *tgbotapi.Message
	Cancel    context.CancelFunc
	StartTime time.Time
}

// TaskManager 任务管理器
type TaskManager struct {
	mu           sync.RWMutex
	batches      map[int64]map[int]*BatchTask // userID -> batchID -> batch
	batchCounter map[int64]int                // userID -> batch counter
	taskCounter  map[int64]int                // userID -> task counter
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		batches:      make(map[int64]map[int]*BatchTask),
		batchCounter: make(map[int64]int),
		taskCounter:  make(map[int64]int),
	}
}

func (tm *TaskManager) AddBatch(batch *BatchTask) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.batches[batch.UserID] == nil {
		tm.batches[batch.UserID] = make(map[int]*BatchTask)
	}
	tm.batches[batch.UserID][batch.BatchID] = batch
}

func (tm *TaskManager) RemoveBatch(userID int64, batchID int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.batches[userID] != nil {
		delete(tm.batches[userID], batchID)
	}
}

func (tm *TaskManager) CancelBatch(userID int64, batchID int) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.batches[userID] == nil {
		return false
	}

	batch, ok := tm.batches[userID][batchID]
	if !ok {
		return false
	}

	// 取消所有任务
	for _, task := range batch.Tasks {
		task.CancelMutex.Lock()
		if !task.Cancelled {
			task.Cancelled = true
			task.Status = "cancelled"
		}
		task.CancelMutex.Unlock()
	}

	// 取消批次的 context
	if batch.Cancel != nil {
		batch.Cancel()
	}

	return true
}

func (tm *TaskManager) GetNextBatchID(userID int64) int {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.batchCounter[userID]++
	return tm.batchCounter[userID]
}

func (tm *TaskManager) GetNextTaskID(userID int64) int {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.taskCounter[userID]++
	return tm.taskCounter[userID]
}

func (tm *TaskManager) GetBatch(userID int64, batchID int) *BatchTask {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.batches[userID] == nil {
		return nil
	}
	return tm.batches[userID][batchID]
}

// StartTelegramBot 启动 Telegram Bot
func (p *MessageProcessor) StartTelegramBot(ctx context.Context) error {
	bot, err := tgbotapi.NewBotAPI(p.config.Bot.Token)
	if err != nil {
		return fmt.Errorf("创建 Bot 失败: %w", err)
	}

	bot.Debug = false
	p.ext.Log().Info(fmt.Sprintf("Bot 已授权: @%s", bot.Self.UserName))
	fmt.Printf("✅ Bot 已授权: @%s\n", bot.Self.UserName)

	// 创建任务管理器
	taskManager := NewTaskManager()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	p.ext.Log().Info("Bot 开始监听消息...")
	fmt.Println("🎧 Bot 开始监听用户消息...")

	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updates:
			// 处理回调查询（按钮点击）
			if update.CallbackQuery != nil {
				go p.handleCallbackQuery(ctx, bot, taskManager, update.CallbackQuery)
				continue
			}

			if update.Message == nil {
				continue
			}

			// 检查用户权限
			if !p.isUserAllowed(update.Message.From.ID) {
				continue
			}

			// 处理消息
			go p.handleBotMessage(ctx, bot, taskManager, update.Message)
		}
	}
}

// isUserAllowed 检查用户是否有权限
func (p *MessageProcessor) isUserAllowed(userID int64) bool {
	if len(p.config.Bot.AllowedUsers) == 0 {
		return true // 没有限制，所有用户都可以使用
	}

	for _, id := range p.config.Bot.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}

// handleBotMessage 处理 Bot 消息
func (p *MessageProcessor) handleBotMessage(ctx context.Context, bot *tgbotapi.BotAPI, taskManager *TaskManager, msg *tgbotapi.Message) {
	text := msg.Text

	// 处理命令
	if strings.HasPrefix(text, "/start") {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"👋 欢迎使用 tdl-msgproce Bot！\n\n"+
				"📌 功能：\n"+
				"• 发送 Telegram 链接进行转发\n"+
				"• 发送订阅链接添加到监听\n\n"+
				"🔗 支持格式:\n"+
				"• https://t.me/channel/123\n"+
				"• @channel_username\n"+
				"• 订阅链接 (http/https)\n"+
				"• 多个链接（空格或换行分隔）")
		return
	}

	if strings.HasPrefix(text, "/help") {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"📖 使用帮助:\n\n"+
				"1️⃣ 转发消息\n"+
				"   • 发送 Telegram 链接进行转发\n"+
				"   • 支持批量转发（一次发送多个链接）\n"+
				fmt.Sprintf("   • 转发目标: %d\n", p.config.Bot.ForwardTarget)+
				fmt.Sprintf("   • 转发模式: %s\n\n", p.config.Bot.ForwardMode)+
				"2️⃣ 添加订阅\n"+
				"   • 发送订阅链接 (http/https 格式)\n"+
				"   • 自动添加到监听系统\n\n"+
				"3️⃣ 查看状态\n"+
				"   • 使用 /status 查看运行状态")
		return
	}

	if strings.HasPrefix(text, "/status") {
		status := fmt.Sprintf("📊 运行状态:\n\n"+
			"✅ Bot: 运行中\n"+
			"📝 处理消息: %d\n"+
			"🔄 转发次数: %d\n"+
			"🎯 转发目标: %d",
			p.messageCount, p.forwardCount, p.config.Bot.ForwardTarget)
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, status)
		return
	}

	// 提取链接或频道用户名
	links := extractTelegramLinks(text)
	if len(links) == 0 {
		// 检查是否是订阅链接（http/https 但不是 t.me）
		if subLink := extractSubscriptionLink(text); subLink != "" {
			p.handleSubscriptionLink(ctx, bot, msg, subLink)
			return
		}

		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"❌ 未找到有效链接\n\n"+
				"请发送以下格式:\n"+
				"• Telegram 链接: https://t.me/channel/123\n"+
				"• 频道用户名: @channel_username\n"+
				"• 订阅链接: http/https 格式")
		return
	}

	// 创建批量任务
	batchID := taskManager.GetNextBatchID(msg.From.ID)
	tasks := make([]*ForwardTask, 0, len(links))

	for _, link := range links {
		taskID := taskManager.GetNextTaskID(msg.From.ID)
		task := &ForwardTask{
			ID:        taskID,
			Link:      link,
			UserID:    msg.From.ID,
			Status:    "pending",
			Cancelled: false,
		}
		tasks = append(tasks, task)
	}

	// 创建取消按钮
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛑 终止所有任务", fmt.Sprintf("cancel_batch_%d_%d", msg.From.ID, batchID)),
		),
	)

	// 发送汇总状态消息
	statusText := p.buildBatchStatusText(batchID, tasks)
	statusMsg := p.sendBotMessageWithKeyboard(bot, msg.Chat.ID, statusText, keyboard)
	if statusMsg == nil {
		return
	}

	// 创建可取消的 context
	batchCtx, cancel := context.WithCancel(ctx)

	// 创建批量任务
	batch := &BatchTask{
		BatchID:   batchID,
		UserID:    msg.From.ID,
		Tasks:     tasks,
		StatusMsg: statusMsg,
		Cancel:    cancel,
		StartTime: time.Now(),
	}

	// 添加到任务管理器
	taskManager.AddBatch(batch)

	// 异步执行批量转发
	go p.executeBatchTasks(batchCtx, bot, taskManager, batch)
}

// sendBotReply 发送回复消息
func (p *MessageProcessor) sendBotReply(bot *tgbotapi.BotAPI, chatID int64, replyToID int, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = replyToID
	bot.Send(msg)
}

// sendBotMessage 发送消息
func (p *MessageProcessor) sendBotMessage(bot *tgbotapi.BotAPI, chatID int64, text string) *tgbotapi.Message {
	msg := tgbotapi.NewMessage(chatID, text)
	sent, err := bot.Send(msg)
	if err != nil {
		log.Printf("发送消息失败: %v", err)
		return nil
	}
	return &sent
}

// updateBotMessage 更新消息
func (p *MessageProcessor) updateBotMessage(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	bot.Send(edit)
}

// handleSubscriptionLink 处理订阅链接或代理节点
func (p *MessageProcessor) handleSubscriptionLink(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, link string) {
	isNode := isProxyNode(link)
	linkType := "订阅链接"
	if isNode {
		linkType = "代理节点"
	}

	p.ext.Log().Info(fmt.Sprintf("检测到%s: %s", linkType, link))

	// 发送处理中消息
	statusMsg := p.sendBotMessage(bot, msg.Chat.ID, fmt.Sprintf("⏳ 正在添加%s...", linkType))
	if statusMsg == nil {
		return
	}

	// 添加订阅或节点到 API
	success, responseMsg := p.addSubscriptionToAPI(link, isNode)

	if success {
		p.ext.Log().Info(fmt.Sprintf("%s添加成功: %s", linkType, link))
	} else {
		p.ext.Log().Info(fmt.Sprintf("%s添加失败: %s", linkType, link))
	}

	// 更新状态消息
	p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID, responseMsg)
}

// extractSubscriptionLink 提取订阅链接（非 t.me 的 http/https）或代理节点链接
func extractSubscriptionLink(text string) string {
	// 查找 http/https 链接但不是 t.me
	parts := strings.Fields(text)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// 检查是否为代理节点链接
		if isProxyNode(part) {
			return part
		}
		// 检查是否为订阅链接（http/https 但不是 t.me）
		if (strings.HasPrefix(part, "http://") || strings.HasPrefix(part, "https://")) &&
			!strings.Contains(part, "t.me") {
			return part
		}
	}
	return ""
}

// isProxyNode 判断是否为代理节点链接
func isProxyNode(link string) bool {
	prefixes := []string{
		"vmess://", "vless://", "ss://", "ssr://",
		"trojan://", "hysteria://", "hysteria2://", "hy2://",
	}
	linkLower := strings.ToLower(link)
	for _, prefix := range prefixes {
		if strings.HasPrefix(linkLower, prefix) {
			return true
		}
	}
	return false
}

// extractTelegramLinks 提取 Telegram 链接
func extractTelegramLinks(text string) []string {
	var links []string

	// 分割文本（空格或换行）
	parts := strings.Fields(text)

	for _, part := range parts {
		part = strings.TrimSpace(part)

		// 匹配 t.me 链接
		if strings.Contains(part, "t.me/") {
			links = append(links, part)
			continue
		}

		// 匹配 @username
		if strings.HasPrefix(part, "@") && len(part) > 1 {
			links = append(links, part)
			continue
		}
	}

	return links
}

// SubscriptionRequest 订阅或节点请求结构
type SubscriptionRequest struct {
	SubURL string `json:"sub_url,omitempty"`
	SS     string `json:"ss,omitempty"`
	Test   bool   `json:"test"`
}

// SubscriptionResponse 订阅响应结构
type SubscriptionResponse struct {
	Message     string `json:"message"`
	Error       string `json:"error"`
	SubURL      string `json:"sub_url"`
	TestedNodes *int   `json:"tested_nodes,omitempty"`
	PassedNodes *int   `json:"passed_nodes,omitempty"`
	FailedNodes *int   `json:"failed_nodes,omitempty"`
	AddedNodes  *int   `json:"added_nodes,omitempty"`
	Duration    string `json:"duration,omitempty"`
	Timeout     *bool  `json:"timeout,omitempty"`
	Warning     string `json:"warning,omitempty"`
}

// addSubscriptionToAPI 添加订阅或节点到 API
func (p *MessageProcessor) addSubscriptionToAPI(link string, isNode bool) (bool, string) {
	if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
		return false, "❌ 订阅 API 未配置"
	}

	apiURL := p.config.Monitor.SubscriptionAPI.AddURL

	var reqBody SubscriptionRequest
	if isNode {
		reqBody.SS = link
	} else {
		reqBody.SubURL = link
	}
	reqBody.Test = true

	linkType := "订阅"
	if isNode {
		linkType = "节点"
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		p.ext.Log().Info("JSON 序列化失败", zap.Error(err))
		return false, fmt.Sprintf("❌ 请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		p.ext.Log().Info("创建请求失败", zap.Error(err))
		return false, fmt.Sprintf("❌ 请求失败: %v", err)
	}

	req.Header.Set("X-API-Key", p.config.Monitor.SubscriptionAPI.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	p.ext.Log().Info(fmt.Sprintf("发送%s请求到 %s", linkType, apiURL))

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		p.ext.Log().Info(fmt.Sprintf("%s API 请求失败", linkType), zap.Error(err))
		return false, "❌ 无法连接到服务器"
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.ext.Log().Info("读取响应失败", zap.Error(err))
		return false, "❌ 读取响应失败"
	}

	// 记录原始响应（用于调试）
	p.ext.Log().Info("API 响应", zap.Int("status", resp.StatusCode), zap.String("body", string(body)))

	var response SubscriptionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		p.ext.Log().Info("解析响应失败",
			zap.Error(err),
			zap.String("body", string(body)),
			zap.Int("status", resp.StatusCode))

		// 如果是 200 状态码但解析失败，可能是纯文本响应
		if resp.StatusCode == 200 {
			return true, fmt.Sprintf("✅ %s添加成功", linkType)
		}
		return false, fmt.Sprintf("❌ %s添加失败 (状态码: %d)", linkType, resp.StatusCode)
	}

	if resp.StatusCode == 200 {
		// 检查是否为检测模式响应
		if response.TestedNodes != nil {
			// 检测模式响应 - 判断是否有节点被添加
			var msg string
			var success bool
			
			// 判断是否有节点被成功添加
			if response.AddedNodes != nil && *response.AddedNodes > 0 {
				// 成功情况：有节点被添加
				success = true
				if isNode {
					msg = "✅ 节点检测并添加成功\n"
				} else {
					msg = "✅ 订阅检测并添加成功\n"
				}
			} else {
				// 失败情况：没有节点被添加
				success = false
				// 使用 API 返回的错误信息，如果没有则使用默认消息
				if response.Error != "" {
					msg = fmt.Sprintf("❌ %s\n", response.Error)
				} else if isNode {
					msg = "❌ 节点检测失败，未添加任何节点\n"
				} else {
					msg = "❌ 订阅检测失败，未添加任何节点\n"
				}
			}
			
			// 添加统计信息（成功和失败都显示）
			msg += fmt.Sprintf("📊 检测: %d个节点\n", *response.TestedNodes)
			if response.PassedNodes != nil {
				msg += fmt.Sprintf("✅ 通过: %d个\n", *response.PassedNodes)
			}
			if response.FailedNodes != nil {
				msg += fmt.Sprintf("❌ 失败: %d个\n", *response.FailedNodes)
			}
			if response.AddedNodes != nil {
				msg += fmt.Sprintf("➕ 添加: %d个\n", *response.AddedNodes)
			}
			if response.Duration != "" {
				msg += fmt.Sprintf("⏱ 耗时: %s", response.Duration)
			}
			if response.Timeout != nil && *response.Timeout && response.Warning != "" {
				msg += "\n⚠️ " + response.Warning
			}
			
			// 记录日志
			if success {
				p.ext.Log().Info(fmt.Sprintf("%s检测并添加成功", linkType),
					zap.String("link", link),
					zap.Int("tested", *response.TestedNodes),
					zap.String("duration", response.Duration))
			} else {
				p.ext.Log().Info(fmt.Sprintf("%s检测失败，未添加节点", linkType),
					zap.String("link", link),
					zap.Int("tested", *response.TestedNodes),
					zap.String("duration", response.Duration))
			}
			
			return success, msg
		} else {
			// 普通模式响应
			successMsg := response.Message
			if successMsg == "" {
				successMsg = fmt.Sprintf("%s添加成功", linkType)
			}
			p.ext.Log().Info(fmt.Sprintf("%s添加成功: %s - %s", linkType, link, successMsg))
			return true, fmt.Sprintf("✅ %s", successMsg)
		}
	}

	// 处理检测失败的情况（400状态码但包含检测统计信息）
	if resp.StatusCode == 400 && response.TestedNodes != nil {
		// 检测模式响应 - 检测失败
		var msg string
		// 使用 API 返回的错误信息，如果没有则使用默认消息
		if response.Error != "" {
			msg = fmt.Sprintf("❌ %s\n", response.Error)
		} else if isNode {
			msg = "❌ 节点检测失败，未添加任何节点\n"
		} else {
			msg = "❌ 订阅检测失败，未添加任何节点\n"
		}
		
		// 添加统计信息
		msg += fmt.Sprintf("📊 检测: %d个节点\n", *response.TestedNodes)
		if response.PassedNodes != nil {
			msg += fmt.Sprintf("✅ 通过: %d个\n", *response.PassedNodes)
		}
		if response.FailedNodes != nil {
			msg += fmt.Sprintf("❌ 失败: %d个\n", *response.FailedNodes)
		}
		if response.AddedNodes != nil {
			msg += fmt.Sprintf("➕ 添加: %d个\n", *response.AddedNodes)
		}
		if response.Duration != "" {
			msg += fmt.Sprintf("⏱️ 耗时: %s", response.Duration)
		}
		if response.Timeout != nil && *response.Timeout && response.Warning != "" {
			msg += "\n⚠️ " + response.Warning
		}
		
		p.ext.Log().Info(fmt.Sprintf("%s检测失败，未添加节点", linkType),
			zap.String("link", link),
			zap.Int("tested", *response.TestedNodes),
			zap.String("duration", response.Duration))
		
		return false, msg
	}

	// 处理重复订阅或节点（409 Conflict）
	if resp.StatusCode == 409 || resp.StatusCode == http.StatusConflict {
		errorMsg := response.Error
		if errorMsg == "" {
			if isNode {
				errorMsg = "节点已存在"
			} else {
				errorMsg = "该订阅链接已存在"
			}
		}
		p.ext.Log().Info(fmt.Sprintf("%s已存在", linkType), zap.String("link", link))
		return false, fmt.Sprintf("⚠️ %s", errorMsg)
	}

	// 其他错误
	errorMsg := response.Error
	if errorMsg == "" {
		errorMsg = response.Message
	}
	if errorMsg == "" {
		errorMsg = fmt.Sprintf("%s添加失败 (状态码: %d)", linkType, resp.StatusCode)
	}

	p.ext.Log().Info(fmt.Sprintf("%s添加失败: %s", linkType, errorMsg))
	return false, fmt.Sprintf("❌ %s", errorMsg)
}

// sendBotMessageWithKeyboard 发送带按钮的消息
func (p *MessageProcessor) sendBotMessageWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) *tgbotapi.Message {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	sent, err := bot.Send(msg)
	if err != nil {
		log.Printf("发送消息失败: %v", err)
		return nil
	}
	return &sent
}

// updateBotMessageWithKeyboard 更新消息和按钮
func (p *MessageProcessor) updateBotMessageWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = &keyboard
	bot.Send(edit)
}

// buildBatchStatusText 构建批量任务状态文本
func (p *MessageProcessor) buildBatchStatusText(batchID int, tasks []*ForwardTask) string {
	var sb strings.Builder

	// 计算当前进度（哪个任务正在运行）
	var currentTaskIndex int
	for idx, t := range tasks {
		if t.Status == "running" {
			currentTaskIndex = idx + 1
			break
		}
	}

	if currentTaskIndex > 0 {
		sb.WriteString(fmt.Sprintf("📦 批次 #%d | 任务: %d/%d\n\n", batchID, currentTaskIndex, len(tasks)))
	} else {
		sb.WriteString(fmt.Sprintf("📦 批次 #%d (%d个任务)\n\n", batchID, len(tasks)))
	}

	for _, task := range tasks {
		var statusIcon string
		var statusText string

		switch task.Status {
		case "pending":
			statusIcon = "⏳"
			statusText = "待处理"
		case "running":
			statusIcon = "🔄"
			statusText = fmt.Sprintf("转发中 %d%%", task.Progress)
		case "completed":
			statusIcon = "✅"
			statusText = "已完成"
		case "cancelled":
			statusIcon = "❌"
			statusText = "已取消"
		case "failed":
			statusIcon = "⚠️"
			if task.Error != "" {
				statusText = task.Error
			} else {
				statusText = "失败"
			}
		default:
			statusIcon = "❓"
			statusText = "未知"
		}

		sb.WriteString(fmt.Sprintf("%s #%d [%s] %s\n", statusIcon, task.ID, statusText, task.Link))
	}

	return sb.String()
}

// executeBatchTasks 执行批量转发任务
func (p *MessageProcessor) executeBatchTasks(ctx context.Context, bot *tgbotapi.BotAPI, taskManager *TaskManager, batch *BatchTask) {
	defer taskManager.RemoveBatch(batch.UserID, batch.BatchID)

	// 创建取消按钮
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛑 终止所有任务", fmt.Sprintf("cancel_batch_%d_%d", batch.UserID, batch.BatchID)),
		),
	)

	// 逐个执行任务
	for i, task := range batch.Tasks {
		// 检查是否已取消
		task.CancelMutex.Lock()
		if task.Cancelled {
			task.CancelMutex.Unlock()
			break
		}
		task.CancelMutex.Unlock()

		// 更新任务状态为运行中
		task.Status = "running"
		task.Progress = 0
		statusText := p.buildBatchStatusText(batch.BatchID, batch.Tasks)
		p.updateBotMessageWithKeyboard(bot, batch.StatusMsg.Chat.ID, batch.StatusMsg.MessageID, statusText, keyboard)

		// 进度更新回调
		lastUpdate := time.Now()
		lastPercent := -1
		onProgress := func(percent int, line string) {
			p.ext.Log().Info("进度回调", zap.Int("percent", percent), zap.String("line", line))
			task.Progress = percent

			// 只在进度变化时保存新行（避免重复）
			if percent != lastPercent {
				task.ProgressMutex.Lock()
				task.ProgressLines = append(task.ProgressLines, fmt.Sprintf("%d%% - %s", percent, line))
				if len(task.ProgressLines) > 5 {
					task.ProgressLines = task.ProgressLines[len(task.ProgressLines)-5:]
				}
				task.ProgressMutex.Unlock()
				lastPercent = percent
			}

			// 限制更新频率，避免过于频繁
			if time.Since(lastUpdate) > 1*time.Second {
				lastUpdate = time.Now()
				p.ext.Log().Info("更新Bot消息", zap.Int("taskID", task.ID), zap.Int("percent", percent))
				statusText := p.buildBatchStatusText(batch.BatchID, batch.Tasks)
				p.updateBotMessageWithKeyboard(bot, batch.StatusMsg.Chat.ID, batch.StatusMsg.MessageID, statusText, keyboard)
			}
		}

		// 执行转发（传入进度回调）
		err := p.forwardFromLink(ctx, task.Link, onProgress)

		// 检查context是否被取消
		if ctx.Err() == context.Canceled {
			task.Status = "cancelled"
			task.Error = "用户终止"
		} else if err != nil {
			task.Status = "failed"
			task.Error = err.Error()
			p.ext.Log().Info("转发失败", zap.Int("taskID", task.ID), zap.String("link", task.Link), zap.Error(err))
		} else {
			task.Status = "completed"
			p.forwardCount++
			p.ext.Log().Info("转发成功", zap.Int("taskID", task.ID), zap.String("link", task.Link))
		}

		// 更新状态显示
		statusText = p.buildBatchStatusText(batch.BatchID, batch.Tasks)

		// 如果是最后一个任务或有任务失败/取消，移除按钮
		if i == len(batch.Tasks)-1 || task.Status == "cancelled" {
			p.updateBotMessage(bot, batch.StatusMsg.Chat.ID, batch.StatusMsg.MessageID, statusText)
		} else {
			p.updateBotMessageWithKeyboard(bot, batch.StatusMsg.Chat.ID, batch.StatusMsg.MessageID, statusText, keyboard)
		}

		// 如果任务被取消，停止执行剩余任务
		if task.Status == "cancelled" {
			// 标记剩余任务为已取消
			for j := i + 1; j < len(batch.Tasks); j++ {
				batch.Tasks[j].Status = "cancelled"
				batch.Tasks[j].Error = "批次已终止"
			}
			break
		}

		// 任务间隔（避免频繁操作）
		if i < len(batch.Tasks)-1 {
			time.Sleep(1 * time.Second)
		}
	}

	// 最终状态统计
	var completed, failed, cancelled int
	for _, task := range batch.Tasks {
		switch task.Status {
		case "completed":
			completed++
		case "failed":
			failed++
		case "cancelled":
			cancelled++
		}
	}

	finalText := fmt.Sprintf("📦 批次 #%d 已完成\n\n"+
		"总计: %d个任务\n"+
		"✅ 成功: %d\n"+
		"⚠️ 失败: %d\n"+
		"❌ 取消: %d\n\n"+
		"耗时: %v",
		batch.BatchID,
		len(batch.Tasks),
		completed,
		failed,
		cancelled,
		time.Since(batch.StartTime).Round(time.Second),
	)

	p.updateBotMessage(bot, batch.StatusMsg.Chat.ID, batch.StatusMsg.MessageID, finalText)
}

// handleCallbackQuery 处理回调查询（按钮点击）
func (p *MessageProcessor) handleCallbackQuery(ctx context.Context, bot *tgbotapi.BotAPI, taskManager *TaskManager, query *tgbotapi.CallbackQuery) {
	// 解析回调数据: cancel_batch_<userID>_<batchID>
	parts := strings.Split(query.Data, "_")
	if len(parts) != 4 || parts[0] != "cancel" || parts[1] != "batch" {
		callback := tgbotapi.NewCallback(query.ID, "⚠️ 无效的操作")
		bot.Request(callback)
		return
	}

	var userID, batchID int64
	fmt.Sscanf(parts[2], "%d", &userID)
	fmt.Sscanf(parts[3], "%d", &batchID)

	// 权限验证
	if query.From.ID != userID {
		callback := tgbotapi.NewCallback(query.ID, "❌ 无权操作他人的任务")
		bot.Request(callback)
		return
	}

	// 取消批量任务
	if taskManager.CancelBatch(userID, int(batchID)) {
		p.ext.Log().Info("用户终止批量任务", zap.Int64("userID", userID), zap.Int64("batchID", batchID))
		callback := tgbotapi.NewCallback(query.ID, "✅ 所有任务已终止")
		bot.Request(callback)
	} else {
		callback := tgbotapi.NewCallback(query.ID, "⚠️ 任务不存在或已完成")
		bot.Request(callback)
	}
}
