package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// handleMessage 处理单个消息
func (p *MessageProcessor) handleMessage(ctx context.Context, msg *tg.Message, entities tg.Entities) error {
	// 检查是否是监听的频道
	peerID := getPeerID(msg.PeerID)
	if !contains(p.config.Monitor.Channels, peerID) {
		return nil
	}

	p.messageCount++

	// 获取消息文本
	text := msg.Message
	if text == "" {
		return nil
	}

	// 关键词过滤
	if !matchAny(text, p.config.Monitor.Filters.Keywords) {
		return nil
	}

	// 白名单频道跳过二次过滤
	isWhitelisted := contains(p.config.Monitor.WhitelistChannels, peerID)
	if !isWhitelisted && len(p.config.Monitor.Filters.ContentFilter) > 0 {
		if !matchAny(text, p.config.Monitor.Filters.ContentFilter) {
			return nil
		}
	}

	// 提取链接
	links := extractLinks(text)
	if len(links) == 0 {
		return nil
	}

	// 过滤黑名单链接
	filteredLinks := filterLinks(links, p.config.Monitor.Filters.LinkBlacklist)
	if len(filteredLinks) == 0 {
		return nil
	}

	// 发送到订阅 API
	for _, link := range filteredLinks {
		if err := p.addSubscription(link); err != nil {
			p.ext.Log().Error("发送订阅失败",
				zap.String("link", link),
				zap.Error(err))
		} else {
			p.ext.Log().Info("新订阅",
				zap.Int64("channel", peerID),
				zap.String("link", link))
			fmt.Printf("✅ 新订阅: %s (频道: %d)\n", link, peerID)
		}
	}

	return nil
}

// fetchChannelHistory 获取频道历史消息
func (p *MessageProcessor) fetchChannelHistory(ctx context.Context, channelID int64) error {
	fmt.Printf("📥 正在获取频道 %d 的历史消息...\n", channelID)

	// 构造 InputPeerChannel
	inputPeer := &tg.InputPeerChannel{
		ChannelID:  channelID,
		AccessHash: 0, // 需要从对话列表中获取
	}

	// 尝试通过 ChannelsGetChannels 获取频道信息
	channel, err := p.api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: 0,
		},
	})

	if err != nil {
		// 如果失败，从对话列表中查找 AccessHash
		dialogs, err := p.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			OffsetDate: 0,
			OffsetID:   0,
			OffsetPeer: &tg.InputPeerEmpty{},
			Limit:      100,
			Hash:       0,
		})

		if err != nil {
			return fmt.Errorf("获取对话列表失败: %w", err)
		}

		// 查找对应的频道
		var accessHash int64
		var foundChannel *tg.Channel
		switch d := dialogs.(type) {
		case *tg.MessagesDialogs:
			for _, chat := range d.Chats {
				if ch, ok := chat.(*tg.Channel); ok && ch.ID == channelID {
					accessHash = ch.AccessHash
					foundChannel = ch
					break
				}
			}
		case *tg.MessagesDialogsSlice:
			for _, chat := range d.Chats {
				if ch, ok := chat.(*tg.Channel); ok && ch.ID == channelID {
					accessHash = ch.AccessHash
					foundChannel = ch
					break
				}
			}
		}

		if foundChannel == nil {
			return fmt.Errorf("未找到频道 %d，请确认已加入该频道", channelID)
		}

		fmt.Printf("📢 频道名称: %s\n", foundChannel.Title)
		inputPeer.AccessHash = accessHash
	} else {
		// 成功获取频道信息
		switch chats := channel.(type) {
		case *tg.MessagesChats:
			if len(chats.Chats) > 0 {
				if ch, ok := chats.Chats[0].(*tg.Channel); ok {
					fmt.Printf("📢 频道名称: %s\n", ch.Title)
					inputPeer.AccessHash = ch.AccessHash
				}
			}
		}
	}

	// 获取历史消息
	history, err := p.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:       inputPeer,
		OffsetID:   0,
		OffsetDate: 0,
		AddOffset:  0,
		Limit:      100, // 获取最近100条
		MaxID:      0,
		MinID:      0,
		Hash:       0,
	})

	if err != nil {
		return fmt.Errorf("获取历史消息失败: %w", err)
	}

	// 处理历史消息
	var messages []tg.MessageClass
	switch h := history.(type) {
	case *tg.MessagesMessages:
		messages = h.Messages
	case *tg.MessagesMessagesSlice:
		messages = h.Messages
	case *tg.MessagesChannelMessages:
		messages = h.Messages
	}

	fmt.Printf("📊 获取到 %d 条历史消息\n", len(messages))

	// 处理每条消息
	matchCount := 0
	for i := len(messages) - 1; i >= 0; i-- { // 倒序处理，从旧到新
		msg, ok := messages[i].(*tg.Message)
		if !ok {
			continue
		}

		// 构建 entities（简化版）
		entities := tg.Entities{
			Users: make(map[int64]*tg.User),
			Chats: make(map[int64]*tg.Chat),
		}

		// 使用现有的 handleMessage 处理
		err := p.handleMessage(ctx, msg, entities)
		if err == nil {
			matchCount++
		}
	}

	fmt.Printf("✅ 频道 %d: 处理了 %d 条消息\n", channelID, matchCount)
	return nil
}

// addSubscription 添加订阅
func (p *MessageProcessor) addSubscription(link string) error {
	if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
		return fmt.Errorf("订阅 API 未配置")
	}

	// 使用配置文件中的完整 URL
	apiURL := p.config.Monitor.SubscriptionAPI.AddURL

	// 构建请求体
	type SubscriptionRequest struct {
		SubURL string `json:"sub_url"`
	}

	reqBody := SubscriptionRequest{SubURL: link}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}

	// 创建请求
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("X-API-Key", p.config.Monitor.SubscriptionAPI.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	type SubscriptionResponse struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}

	var response SubscriptionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("解析响应失败 (状态码: %d): %w", resp.StatusCode, err)
	}

	// 处理响应
	if resp.StatusCode == 200 {
		successMsg := response.Message
		if successMsg == "" {
			successMsg = "订阅添加成功"
		}
		p.ext.Log().Info("订阅添加成功", zap.String("link", link), zap.String("message", successMsg))
		return nil
	}

	// 错误处理
	errorMsg := response.Error
	if errorMsg == "" {
		errorMsg = response.Message
	}
	if errorMsg == "" {
		errorMsg = fmt.Sprintf("订阅添加失败 (状态码: %d)", resp.StatusCode)
	}

	// 特殊处理重复订阅（不作为错误）
	if strings.Contains(errorMsg, "已存在") || strings.Contains(strings.ToLower(errorMsg), "already exists") {
		p.ext.Log().Debug("订阅已存在", zap.String("link", link))
		return nil // 不返回错误，避免重复日志
	}

	return fmt.Errorf("%s", errorMsg)
}

// 辅助函数
func getPeerID(peer tg.PeerClass) int64 {
	switch p := peer.(type) {
	case *tg.PeerChannel:
		return p.ChannelID
	case *tg.PeerChat:
		return p.ChatID
	case *tg.PeerUser:
		return p.UserID
	}
	return 0
}

func contains(slice []int64, val int64) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func matchAny(text string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	text = strings.ToLower(text)
	for _, pattern := range patterns {
		if strings.Contains(text, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func extractLinks(text string) []string {
	// 匹配 http/https 链接，支持中文标点
	re := regexp.MustCompile(`https?://[^\s\x{FF0C}\x{3002}\x{FF1F}\x{FF01}\x{FF1B}\x{FF1A}\x{201C}\x{201D}\x{2018}\x{2019}]+`)
	return re.FindAllString(text, -1)
}

func filterLinks(links []string, blacklist []string) []string {
	var filtered []string
	for _, link := range links {
		blocked := false
		for _, keyword := range blacklist {
			if strings.Contains(strings.ToLower(link), strings.ToLower(keyword)) {
				blocked = true
				break
			}
		}
		if !blocked {
			filtered = append(filtered, link)
		}
	}
	return filtered
}
