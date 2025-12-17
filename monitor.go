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
	// 消息去重逻辑
	if p.messageCache.Has(msg.ID) {
		p.ext.Log().Debug("消息重复，已跳过", zap.Int("message_id", msg.ID))
		return nil
	}
	p.messageCache.Add(msg.ID) // 存入缓存

	peerID := getPeerID(msg.PeerID)

	// 检查是否是监听的频道
	// 仅当配置文件中的频道列表（channels）不为空时，才进行过滤
	if len(p.config.Monitor.Channels) > 0 {
		if !contains(p.config.Monitor.Channels, peerID) {
			// 如果消息的来源频道/群组不在监听列表中，则直接跳过，不处理
			return nil
		}
	}
	// 如果 `channels` 列表为空，则默认处理所有接收到的频道/群组消息

	// 打印调试日志
	p.ext.Log().Info("处理新消息", zap.Int("id", msg.ID), zap.Int64("channel_id", peerID), zap.String("content", msg.Message))
	fmt.Printf("📨 正在处理消息: ID=%d, ChannelID=%d, 内容=\"%.50s...\"\n", msg.ID, peerID, msg.Message)

	p.messageCount++

	// 获取消息文本
	text := msg.Message
	if text == "" {
		return nil
	}

	// 检查是否包含订阅格式或节点格式
	hasSubsFormat := matchAny(text, p.config.Monitor.Filters.Subs)
	hasNodeFormat := matchAny(text, p.config.Monitor.Filters.SS)

	if !hasSubsFormat && !hasNodeFormat {
		return nil // 既不是订阅也不是节点，跳过
	}

	// 白名单频道跳过二次过滤
	isWhitelisted := contains(p.config.Monitor.WhitelistChannels, peerID)

	// 仅对订阅格式进行二次内容过滤（节点格式不进行二次过滤）
	if hasSubsFormat && !hasNodeFormat {
		// 纯订阅格式，需要二次过滤
		if !isWhitelisted && len(p.config.Monitor.Filters.ContentFilter) > 0 {
			if !matchAny(text, p.config.Monitor.Filters.ContentFilter) {
				return nil
			}
		}
	}
	// 如果是节点格式（hasNodeFormat为true），则跳过二次过滤

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
			linkType := "订阅"
			if isProxyNode(link) {
				linkType = "节点"
			}
			p.ext.Log().Info(fmt.Sprintf("新%s", linkType),
				zap.Int64("channel", peerID),
				zap.String("link", link))
			fmt.Printf("✅ 新%s: %s (频道: %d)\n", linkType, link, peerID)
		}
	}

	return nil
}

// addSubscription 添加订阅或单个节点
func (p *MessageProcessor) addSubscription(link string) error {
	if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
		return fmt.Errorf("订阅 API 未配置")
	}

	// 使用配置文件中的完整 URL
	apiURL := p.config.Monitor.SubscriptionAPI.AddURL

	// 判断是订阅链接还是单个节点
	isNodeLink := isProxyNode(link)

	// 构建请求体
	type SubscriptionRequest struct {
		SubURL string `json:"sub_url,omitempty"`
		SS     string `json:"ss,omitempty"`
	}

	var reqBody SubscriptionRequest
	if isNodeLink {
		reqBody.SS = link
	} else {
		reqBody.SubURL = link
	}

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

	// 记录原始响应（用于调试）
	linkType := "订阅"
	if isNodeLink {
		linkType = "节点"
	}
	p.ext.Log().Debug("API 响应",
		zap.String("type", linkType),
		zap.Int("status", resp.StatusCode),
		zap.String("body", string(body)))

	// 解析响应
	type SubscriptionResponse struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		SubURL  string `json:"sub_url"`
	}

	var response SubscriptionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		p.ext.Log().Error("解析响应失败",
			zap.Error(err),
			zap.String("body", string(body)),
			zap.Int("status", resp.StatusCode))

		// 如果是 200 状态码但解析失败，可能是纯文本响应，视为成功
		if resp.StatusCode == 200 {
			p.ext.Log().Info(linkType+"添加成功（纯文本响应）", zap.String("link", link))
			return nil
		}
		return fmt.Errorf("解析响应失败 (状态码: %d): %w", resp.StatusCode, err)
	}

	// 处理响应
	if resp.StatusCode == 200 {
		successMsg := response.Message
		if successMsg == "" {
			successMsg = linkType + "添加成功"
		}
		p.ext.Log().Info(linkType+"添加成功", zap.String("link", link), zap.String("message", successMsg))
		return nil
	}

	// 处理重复（409 Conflict）- 不作为错误
	if resp.StatusCode == 409 || resp.StatusCode == http.StatusConflict {
		errorMsg := response.Error
		if errorMsg == "" {
			if isNodeLink {
				errorMsg = "节点已存在"
			} else {
				errorMsg = "该订阅链接已存在"
			}
		}
		p.ext.Log().Debug(linkType+"已存在", zap.String("link", link))
		return nil // 不返回错误，避免重复日志
	}

	// 其他错误处理
	errorMsg := response.Error
	if errorMsg == "" {
		errorMsg = response.Message
	}
	if errorMsg == "" {
		errorMsg = fmt.Sprintf(linkType+"添加失败 (状态码: %d)", resp.StatusCode)
	}

	return fmt.Errorf("%s", errorMsg)
}

// fetchChannelHistory 获取频道历史消息
func (p *MessageProcessor) fetchChannelHistory(ctx context.Context, channelID int64, limit int) error {
	fmt.Printf("📥 正在获取频道 %d 的历史消息（最多 %d 条）...\n", channelID, limit)

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
		Limit:      limit, // 使用配置的数量
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

		// >>>>>>>>> 新增日志：打印并记录完整的历史消息 <<<<<<<<<<<
		// 实现去重逻辑
		if p.messageCache.Has(msg.ID) {
			continue // 如果已处理，则跳过
		}
		p.messageCache.Add(msg.ID)

		fmt.Printf("📜 正在处理历史消息: ID=%d, PeerID=%d <<<<<\n", msg.ID, getPeerID(msg.PeerID))
		p.ext.Log().Info("收到历史消息 [RAW]", zap.Any("message_object", msg))

		// 构建 entities（简化版）
		entities := tg.Entities{
			Users: make(map[int64]*tg.User),
			Chats: make(map[int64]*tg.Chat),
		}

		// 使用现有的 handleMessage 处理
		err = p.handleMessage(ctx, msg, entities)
		if err == nil {
			matchCount++
		}
	}

	fmt.Printf("✅ 频道 %d: 处理了 %d 条消息\n", channelID, matchCount)
	return nil
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
