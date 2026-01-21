package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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

			// 处理文档文件（JSON 文件）
			if update.Message.Document != nil {
				go p.handleDocumentMessage(ctx, bot, taskManager, update.Message)
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
				"• 直接发送 JSON 文件进行批量转发\n"+
				"• 发送订阅链接添加到监听\n\n"+
				"🔗 支持格式:\n"+
				"• https://t.me/channel/123\n"+
				"• @channel_username\n"+
				"• 📄 目标ID.json\n"+
				"• 订阅链接 (http/https)\n"+
				"• 多个链接（空格或换行分隔）")
		return
	}

	if strings.HasPrefix(text, "/help") {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"📖 使用帮助:\n\n"+
				"1️⃣ 转发消息\n"+
				"   • 🔥 直接发送 JSON 文件（文件名=目标ID）\n"+
				"   • 例如：123456789.json 转发到 123456789\n"+
				"   • 发送 Telegram 链接进行转发\n"+
				"   • 支持批量转发（一次发送多个链接）\n"+
				fmt.Sprintf("   • 默认目标: %d\n", p.config.Bot.ForwardTarget)+
				fmt.Sprintf("   • 转发模式: %s\n\n", p.config.Bot.ForwardMode)+
				"2️⃣ 添加订阅\n"+
				"   • 发送订阅链接 (http/https 格式)\n"+
				"   • 自动添加到监听系统\n\n"+
				"3️⃣ SS 配置管理\n"+
				"   • /ss config - 查看 SS 配置\n"+
				"   • /ss auto - 自动安装/重置 SS\n\n"+
				"4️⃣ 查看状态\n"+
				"   • 使用 /status 查看运行状态\n\n"+
				"💡 提示：文件名即为转发目标，发送JSON文件后会自动验证和清理无效消息！")
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

	// 处理 /ss 命令
	if strings.HasPrefix(text, "/ss") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
				"❌ 用法错误\n\n"+
					"使用方法: /ss [config|auto]\n\n"+
					"• /ss config - 查看 SS 配置\n"+
					"• /ss auto - 自动安装/重置 SS")
			return
		}

		subCmd := parts[1]
		// 验证子命令（白名单）
		if subCmd != "config" && subCmd != "auto" {
			p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
				"❌ 无效的子命令\n\n"+
					"支持的命令:\n"+
					"• /ss config - 查看 SS 配置\n"+
					"• /ss auto - 自动安装/重置 SS")
			return
		}

		// 发送执行中的提示
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			fmt.Sprintf("⏳ 正在执行 /ss %s...\n\n下载并执行脚本中，最多等待 5 分钟", subCmd))

		// 异步执行脚本
		go func() {
			p.ext.Log().Info("执行 SS 命令",
				zap.Int64("userID", msg.From.ID),
				zap.String("command", subCmd))

			output, err := p.executeSSCommand(ctx, subCmd)
			if err != nil {
				p.ext.Log().Error("SS 命令执行失败",
					zap.Error(err),
					zap.String("command", subCmd))
				p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
					fmt.Sprintf("❌ 执行失败:\n\n%s", err.Error()))
				return
			}

			// 截断输出到 4000 字符（Telegram 限制）
			if len(output) > 4000 {
				output = output[:3900] + "\n\n... (输出过长已截断)"
			}

			p.ext.Log().Info("SS 命令执行成功",
				zap.String("command", subCmd))

			p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
				fmt.Sprintf("✅ 执行完成:\n\n%s", output))
		}()
		return
	}

	// 提取链接或频道用户名
	links := extractTelegramLinks(text)
	if len(links) == 0 {
		// 检查是否是订阅链接或节点链接
		allLinks := p.ExtractAllLinks(text)
		if len(allLinks) > 0 {
			// 过滤非 t.me 链接
			nonTgLinks := make([]string, 0)
			for _, link := range allLinks {
				if !strings.Contains(link, "t.me") {
					nonTgLinks = append(nonTgLinks, link)
				}
			}
			if len(nonTgLinks) > 0 {
				p.handleSubscriptionLinks(ctx, bot, msg, nonTgLinks)
				return
			}
		}

		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"❌ 未找到有效链接\n\n"+
				"请发送以下格式:\n"+
				"• Telegram 链接: https://t.me/channel/123\n"+
				"• 频道用户名: @channel_username\n"+
				"• 订阅链接: http/https 格式\n\n"+
				"💡 批量转发请直接发送 JSON 文件")
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
// addNodesBatchToAPI 批量添加节点到 API
func (p *MessageProcessor) addNodesBatchToAPI(nodes []string) (bool, *SubscriptionResponse) {
	if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
		return false, nil
	}

	if len(nodes) == 0 {
		return false, nil
	}

	apiURL := p.config.Monitor.SubscriptionAPI.AddURL

	// 将多个节点用\n连接
	batchSS := strings.Join(nodes, "\n")

	reqBody := SubscriptionRequest{
		SS:   batchSS,
		Test: true,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		p.ext.Log().Info("JSON 序列化失败", zap.Error(err))
		return false, nil
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		p.ext.Log().Info("创建请求失败", zap.Error(err))
		return false, nil
	}

	req.Header.Set("X-API-Key", p.config.Monitor.SubscriptionAPI.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	p.ext.Log().Info(fmt.Sprintf("发送批量节点请求到 %s，共 %d 个节点", apiURL, len(nodes)))

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		p.ext.Log().Info("批量节点 API 请求失败", zap.Error(err))
		return false, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.ext.Log().Info("读取响应失败", zap.Error(err))
		return false, nil
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
			return true, nil
		}
		return false, nil
	}

	if resp.StatusCode == 200 || resp.StatusCode == 409 {
		// 记录日志
		if response.TestedNodes != nil {
			p.ext.Log().Info("批量节点检测完成",
				zap.Int("count", len(nodes)),
				zap.Int("tested", *response.TestedNodes),
				zap.String("duration", response.Duration))
		} else {
			p.ext.Log().Info("批量节点添加成功", zap.Int("count", len(nodes)))
		}
		return true, &response
	}

	// 处理检测失败的情况
	if resp.StatusCode == 400 && response.TestedNodes != nil {
		p.ext.Log().Info("批量节点检测失败",
			zap.Int("count", len(nodes)),
			zap.Int("tested", *response.TestedNodes),
			zap.String("duration", response.Duration))
		return false, &response
	}

	// 其他错误
	p.ext.Log().Info("批量节点提交失败", zap.String("error", response.Error))
	return false, &response
}

// handleSubscriptionLinks 处理多个订阅/节点链接
func (p *MessageProcessor) handleSubscriptionLinks(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, links []string) {
	// 发送处理中消息
	statusMsg := p.sendBotMessage(bot, msg.Chat.ID, fmt.Sprintf("⏳ 正在处理 %d 个链接...", len(links)))
	if statusMsg == nil {
		return
	}

	// 分组：订阅和节点
	var subscriptions []string
	var nodes []string

	for _, link := range links {
		if p.IsProxyNode(link) {
			nodes = append(nodes, link)
		} else {
			subscriptions = append(subscriptions, link)
		}
	}

	// 合并结果统计
	var allResponses []*SubscriptionResponse
	var totalDurationSeconds float64

	// 处理订阅（逐个提交）
	for _, subLink := range subscriptions {
		p.ext.Log().Info("检测到订阅: " + subLink)
		success, responseMsg := p.addSubscriptionToAPI(subLink, false)

		if success {
			p.ext.Log().Info("订阅添加成功: " + subLink)
		} else {
			p.ext.Log().Error("订阅添加失败: " + subLink + " - " + responseMsg)
		}

		// 解析响应统计信息
		if strings.Contains(responseMsg, "📊") {
			var resp SubscriptionResponse
			// 从消息中提取统计数据
			lines := strings.Split(responseMsg, "\n")
			for _, line := range lines {
				if strings.Contains(line, "📊 检测:") {
					var tested int
					fmt.Sscanf(line, "📊 检测: %d个节点", &tested)
					resp.TestedNodes = &tested
				} else if strings.Contains(line, "✅ 通过:") {
					var passed int
					fmt.Sscanf(line, "✅ 通过: %d个", &passed)
					resp.PassedNodes = &passed
				} else if strings.Contains(line, "❌ 失败:") {
					var failed int
					fmt.Sscanf(line, "❌ 失败: %d个", &failed)
					resp.FailedNodes = &failed
				} else if strings.Contains(line, "➕ 添加:") {
					var added int
					fmt.Sscanf(line, "➕ 添加: %d个", &added)
					resp.AddedNodes = &added
				} else if strings.Contains(line, "⏱") {
					idx := strings.Index(line, ":")
					if idx > 0 {
						resp.Duration = strings.TrimSpace(line[idx+1:])
						// 解析耗时（假设格式为 "1.23s" 或 "123ms"）
						if strings.HasSuffix(resp.Duration, "s") {
							var sec float64
							fmt.Sscanf(resp.Duration, "%fs", &sec)
							totalDurationSeconds += sec
						} else if strings.HasSuffix(resp.Duration, "ms") {
							var ms float64
							fmt.Sscanf(resp.Duration, "%fms", &ms)
							totalDurationSeconds += ms / 1000
						}
					}
				}
			}
			if resp.TestedNodes != nil {
				allResponses = append(allResponses, &resp)
			}
		}
	}

	// 处理节点（批量提交）
	if len(nodes) > 0 {
		p.ext.Log().Info(fmt.Sprintf("检测到%d个节点，准备批量提交", len(nodes)))
		success, resp := p.addNodesBatchToAPI(nodes)

		if success {
			p.ext.Log().Info(fmt.Sprintf("批量节点添加成功: %d个", len(nodes)))
		} else {
			p.ext.Log().Error(fmt.Sprintf("批量节点添加失败: %d个", len(nodes)))
		}

		if resp != nil && resp.TestedNodes != nil {
			allResponses = append(allResponses, resp)
			// 解析耗时
			if resp.Duration != "" {
				if strings.HasSuffix(resp.Duration, "s") {
					var sec float64
					fmt.Sscanf(resp.Duration, "%fs", &sec)
					totalDurationSeconds += sec
				} else if strings.HasSuffix(resp.Duration, "ms") {
					var ms float64
					fmt.Sscanf(resp.Duration, "%fms", &ms)
					totalDurationSeconds += ms / 1000
				}
			}
		}
	}

	// 构造最终消息
	var finalMsg string
	if len(allResponses) > 0 {
		// 合并统计数据
		var totalStats struct {
			TestedNodes int
			PassedNodes int
			FailedNodes int
			AddedNodes  int
		}

		for _, resp := range allResponses {
			if resp.TestedNodes != nil {
				totalStats.TestedNodes += *resp.TestedNodes
			}
			if resp.PassedNodes != nil {
				totalStats.PassedNodes += *resp.PassedNodes
			}
			if resp.FailedNodes != nil {
				totalStats.FailedNodes += *resp.FailedNodes
			}
			if resp.AddedNodes != nil {
				totalStats.AddedNodes += *resp.AddedNodes
			}
		}

		// 生成汇总消息
		finalMsg = "✅检测完成\n"
		finalMsg += fmt.Sprintf("📊 检测: %d个节点\n", totalStats.TestedNodes)
		finalMsg += fmt.Sprintf("✅ 通过: %d个\n", totalStats.PassedNodes)
		finalMsg += fmt.Sprintf("❌ 失败: %d个\n", totalStats.FailedNodes)
		finalMsg += fmt.Sprintf("➕ 添加: %d个\n", totalStats.AddedNodes)
		if totalDurationSeconds > 0 {
			finalMsg += fmt.Sprintf("⏱ 耗时: %.2fs", totalDurationSeconds)
		}
	} else {
		finalMsg = "❌ 处理失败，未获取到有效响应"
	}

	// 更新状态消息
	p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID, finalMsg)
}

// handleSubscriptionLink 处理单个订阅/节点链接 (保持兼容性)
func (p *MessageProcessor) handleSubscriptionLink(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, link string) {
	p.handleSubscriptionLinks(ctx, bot, msg, []string{link})
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
	var runningTask *ForwardTask
	for idx, t := range tasks {
		if t.Status == "running" {
			currentTaskIndex = idx + 1
			runningTask = t
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
		var taskType string

		// 判断任务类型
		if strings.HasSuffix(task.Link, ".json") {
			taskType = "📁"
		} else {
			taskType = "🔗"
		}

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

		// 显示任务信息，文件路径只显示文件名
		displayLink := task.Link
		if strings.HasSuffix(task.Link, ".json") {
			// 提取文件名
			parts := strings.Split(task.Link, "/")
			if len(parts) > 0 {
				displayLink = parts[len(parts)-1]
			}
			// 如果是 Windows 路径
			parts = strings.Split(displayLink, "\\")
			if len(parts) > 0 {
				displayLink = parts[len(parts)-1]
			}
		}

		sb.WriteString(fmt.Sprintf("%s %s #%d [%s] %s\n", statusIcon, taskType, task.ID, statusText, displayLink))
	}

	// 如果有正在运行的文件任务，添加额外提示
	if runningTask != nil && strings.HasSuffix(runningTask.Link, ".json") {
		sb.WriteString("\n💡 大规模迁移进行中，请保持耐心...")
	}

	return sb.String()
}

// executeBatchTasksWithTarget 执行批量转发任务（带自定义目标）
func (p *MessageProcessor) executeBatchTasksWithTarget(ctx context.Context, bot *tgbotapi.BotAPI, taskManager *TaskManager, batch *BatchTask, customTarget int64) {
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

		// 判断是否是文件任务（需要更长的更新间隔）
		isFileTask := strings.HasSuffix(task.Link, ".json")
		updateInterval := 1 * time.Second
		if isFileTask {
			// 文件任务使用更长的更新间隔，避免 Telegram API 限制
			updateInterval = 30 * time.Second
		}

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

			// 限制更新频率，根据任务类型使用不同间隔
			if time.Since(lastUpdate) > updateInterval {
				lastUpdate = time.Now()
				p.ext.Log().Info("更新Bot消息", zap.Int("taskID", task.ID), zap.Int("percent", percent))
				statusText := p.buildBatchStatusText(batch.BatchID, batch.Tasks)
				p.updateBotMessageWithKeyboard(bot, batch.StatusMsg.Chat.ID, batch.StatusMsg.MessageID, statusText, keyboard)
			}
		}

		// 执行转发（传入进度回调和自定义目标）
		err := p.forwardFromLinkWithTarget(ctx, task.Link, customTarget, onProgress)

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

		// 判断是否是文件任务（需要更长的更新间隔）
		isFileTask := strings.HasSuffix(task.Link, ".json")
		updateInterval := 1 * time.Second
		if isFileTask {
			// 文件任务使用更长的更新间隔，避免 Telegram API 限制
			updateInterval = 30 * time.Second
		}

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

			// 限制更新频率，根据任务类型使用不同间隔
			if time.Since(lastUpdate) > updateInterval {
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

// downloadSSScript 从 GitHub 下载脚本到临时文件
func (p *MessageProcessor) downloadSSScript() (string, error) {
	const scriptURL = "https://raw.githubusercontent.com/55gY/cmd/main/cmd.sh"

	// 验证 HTTPS
	if !strings.HasPrefix(scriptURL, "https://") {
		return "", fmt.Errorf("安全错误：仅允许 HTTPS URL")
	}

	// 创建 HTTP 客户端（参考现有代码模式）
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequest("GET", scriptURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	p.ext.Log().Info("下载 SS 脚本", zap.String("url", scriptURL))

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败，HTTP %d", resp.StatusCode)
	}

	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "cmd-*.sh")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()

	// 写入脚本内容
	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("写入脚本失败: %w", err)
	}

	// 设置可执行权限（仅 Unix 系统）
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0700); err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("设置执行权限失败: %w", err)
		}
	}

	p.ext.Log().Info("脚本下载成功", zap.String("tmpPath", tmpPath))
	return tmpPath, nil
}

// executeSSCommand 执行 SS 命令（下载脚本并执行）
func (p *MessageProcessor) executeSSCommand(ctx context.Context, subCmd string) (string, error) {
	// 下载脚本
	tmpPath, err := p.downloadSSScript()
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpPath) // 确保清理临时文件

	// 创建 5 分钟超时的 context
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// 检测系统并找到 bash
	var bashPath string
	if runtime.GOOS == "windows" {
		// Windows: 查找 bash（Git Bash 或 WSL）
		if path, err := exec.LookPath("bash"); err == nil {
			bashPath = path
		} else {
			return "", fmt.Errorf("Windows 系统需要 Git Bash 或 WSL\n请安装 Git for Windows: https://git-scm.com/")
		}
	} else {
		// Linux/macOS
		bashPath = "/bin/bash"
	}

	p.ext.Log().Info("执行脚本",
		zap.String("bash", bashPath),
		zap.String("script", tmpPath),
		zap.String("subCmd", subCmd))

	// 执行脚本：bash tmpPath ss subCmd
	cmd := exec.CommandContext(execCtx, bashPath, tmpPath, "ss", subCmd)

	// 捕获标准输出和错误输出
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 执行命令
	err = cmd.Run()

	// 合并输出
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}

	// 移除 ANSI 颜色代码（如 [0;34m, [0;32m, [0m, [1;33m 等）
	// 匹配 ESC[ 序列和简化的 [ 序列
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m|\[0;[0-9]+m|\[1;[0-9]+m|\[0m`)
	output = ansiRegex.ReplaceAllString(output, "")

	// 检查错误
	if err != nil {
		// 检查是否超时
		if execCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("脚本执行超过 5 分钟已终止")
		}
		// 返回错误和输出
		if output != "" {
			return "", fmt.Errorf("脚本执行失败: %w\n\n输出:\n%s", err, output)
		}
		return "", fmt.Errorf("脚本执行失败: %w", err)
	}

	return output, nil
}

// handleDocumentMessage 处理文档文件消息
func (p *MessageProcessor) handleDocumentMessage(ctx context.Context, bot *tgbotapi.BotAPI, taskManager *TaskManager, msg *tgbotapi.Message) {
	doc := msg.Document
	
	// 检查文件类型
	if !strings.HasSuffix(doc.FileName, ".json") {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"❌ 仅支持 .json 文件\n\n"+
				"请发送格式：目标ID.json\n"+
				"例如：123456789.json")
		return
	}
	
	// 从文件名提取转发目标 ID
	fileNameWithoutExt := strings.TrimSuffix(doc.FileName, ".json")
	var forwardTarget int64
	if _, err := fmt.Sscanf(fileNameWithoutExt, "%d", &forwardTarget); err != nil {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"❌ 文件名格式错误\n\n"+
				"文件名必须是目标ID\n"+
				"例如：123456789.json\n\n"+
				"当前文件名："+doc.FileName)
		return
	}
	
	// 检查文件大小（限制 100MB）
	if doc.FileSize > 100*1024*1024 {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
			"❌ 文件过大\n\n"+
				fmt.Sprintf("文件大小: %.2f MB\n", float64(doc.FileSize)/(1024*1024))+
				"最大限制: 100 MB")
		return
	}
	
	p.ext.Log().Info("收到文档文件",
		zap.String("fileName", doc.FileName),
		zap.Int("fileSize", doc.FileSize),
		zap.Int64("userID", msg.From.ID),
		zap.Int64("forwardTarget", forwardTarget))
	
	// 发送下载中提示
	statusMsg := p.sendBotMessage(bot, msg.Chat.ID,
		fmt.Sprintf("📥 正在下载文件: %s\n文件大小: %.2f MB\n转发目标: %d\n\n请稍候...",
			doc.FileName,
			float64(doc.FileSize)/(1024*1024),
			forwardTarget))
	
	// 获取文件下载链接
	fileConfig := tgbotapi.FileConfig{FileID: doc.FileID}
	file, err := bot.GetFile(fileConfig)
	if err != nil {
		p.ext.Log().Error("获取文件失败", zap.Error(err))
		p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID,
			"❌ 获取文件失败: "+err.Error())
		return
	}
	
	// 使用当前目录，文件名保持不变
	tmpFilePath := doc.FileName
	
	// 下载文件
	fileURL := file.Link(bot.Token)
	resp, err := http.Get(fileURL)
	if err != nil {
		p.ext.Log().Error("下载文件失败", zap.Error(err))
		p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID,
			"❌ 下载文件失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	
	// 保存文件
	outFile, err := os.Create(tmpFilePath)
	if err != nil {
		p.ext.Log().Error("创建文件失败", zap.Error(err))
		p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID,
			"❌ 创建文件失败: "+err.Error())
		return
	}
	
	written, err := io.Copy(outFile, resp.Body)
	outFile.Close()
	if err != nil {
		p.ext.Log().Error("保存文件失败", zap.Error(err))
		os.Remove(tmpFilePath)
		p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID,
			"❌ 保存文件失败: "+err.Error())
		return
	}
	
	p.ext.Log().Info("文件下载成功",
		zap.String("filePath", tmpFilePath),
		zap.Int64("size", written))
	
	// 更新状态 - 文件下载完成
	p.updateBotMessage(bot, statusMsg.Chat.ID, statusMsg.MessageID,
		fmt.Sprintf("✅ 文件下载成功\n\n文件: %s\n大小: %.2f MB\n转发目标: %d\n\n🚀 准备开始转发...",
			doc.FileName,
			float64(written)/(1024*1024),
			forwardTarget))
	
	time.Sleep(2 * time.Second)
	
	// 直接使用原始文件
	finalFilePath := tmpFilePath
	
	// 创建转发任务
	batchID := taskManager.GetNextBatchID(msg.From.ID)
	taskID := taskManager.GetNextTaskID(msg.From.ID)
	
	task := &ForwardTask{
		ID:        taskID,
		Link:      finalFilePath, // 使用清理后的文件路径
		UserID:    msg.From.ID,
		Status:    "pending",
		Cancelled: false,
	}
	
	tasks := []*ForwardTask{task}
	
	// 发送警告提示
	p.sendBotReply(bot, msg.Chat.ID, msg.MessageID,
		"⚠️ 批量转发任务\n\n"+
			"📌 注意事项：\n"+
			"• 大规模迁移可能需要数小时甚至数天\n"+
			"• 程序无超时限制，会持续运行直到完成\n"+
			"• 可随时点击按钮终止任务\n"+
			"• 建议保持程序稳定运行\n"+
			"• Bot 会定期更新进度（每30秒）\n"+
			"• 任务完成后将自动删除文件\n\n"+
			"即将开始执行...")
	time.Sleep(3 * time.Second)
	
	// 创建取消按钮
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🛑 终止所有任务", fmt.Sprintf("cancel_batch_%d_%d", msg.From.ID, batchID)),
		),
	)
	
	// 发送汇总状态消息
	statusText := p.buildBatchStatusText(batchID, tasks)
	batchStatusMsg := p.sendBotMessageWithKeyboard(bot, msg.Chat.ID, statusText, keyboard)
	if batchStatusMsg == nil {
		os.Remove(tmpFilePath) // 清理文件
		return
	}
	
	// 创建可取消的 context
	batchCtx, cancel := context.WithCancel(ctx)
	
	// 创建批量任务
	batch := &BatchTask{
		BatchID:   batchID,
		UserID:    msg.From.ID,
		Tasks:     tasks,
		StatusMsg: batchStatusMsg,
		Cancel:    cancel,
		StartTime: time.Now(),
	}
	
	// 添加到任务管理器
	taskManager.AddBatch(batch)
	
	// 异步执行批量转发（使用自定义转发目标）
	go func() {
		p.executeBatchTasksWithTarget(batchCtx, bot, taskManager, batch, forwardTarget)
		// 任务完成后清理文件
		time.Sleep(2 * time.Second) // 等待最后的状态更新
		
		// 删除文件
		if err := os.Remove(finalFilePath); err != nil {
			p.ext.Log().Warn("删除文件失败",
				zap.String("filePath", finalFilePath),
				zap.Error(err))
		} else {
			p.ext.Log().Info("文件已删除", zap.String("filePath", finalFilePath))
		}
	}()
}
