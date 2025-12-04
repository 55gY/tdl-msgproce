package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

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
	inputPeer := &tg.InputPeerChannel{
		ChannelID:  channelID,
		AccessHash: 0, // 通常需要从缓存获取
	}

	history, err := p.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  inputPeer,
		Limit: 100,
	})
	if err != nil {
		return err
	}

	switch h := history.(type) {
	case *tg.MessagesChannelMessages:
		// 简化处理，直接处理消息
		for _, msg := range h.Messages {
			if m, ok := msg.(*tg.Message); ok {
				// 构建简单的 entities（如果需要的话）
				entities := tg.Entities{
					Users: make(map[int64]*tg.User),
					Chats: make(map[int64]*tg.Chat),
				}
				// 填充 users
				for _, user := range h.Users {
					if u, ok := user.(*tg.User); ok {
						entities.Users[u.ID] = u
					}
				}
				// 填充 chats
				for _, chat := range h.Chats {
					if c, ok := chat.(*tg.Chat); ok {
						entities.Chats[c.ID] = c
					} else if ch, ok := chat.(*tg.Channel); ok {
						// Channel 转换为 Chat 的方式
						entities.Chats[ch.ID] = &tg.Chat{ID: ch.ID, Title: ch.Title}
					}
				}
				p.handleMessage(ctx, m, entities)
			}
		}
	}

	return nil
}

// addSubscription 添加订阅
func (p *MessageProcessor) addSubscription(link string) error {
	// 使用配置文件中的完整 URL
	url := p.config.Monitor.SubscriptionAPI.AddURL

	payload := map[string]interface{}{
		"url": link,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Key", p.config.Monitor.SubscriptionAPI.ApiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API 返回错误状态码: %d", resp.StatusCode)
	}

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
