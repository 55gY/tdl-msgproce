package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// handleMessage 处理新消息（非编辑），返回 (有效订阅数, 有效节点数, error)
func (p *MessageProcessor) handleMessage(ctx context.Context, msg *tg.Message, entities tg.Entities) (int, int, error) {
	peerID := getPeerID(msg.PeerID)


	// 获取编辑时间（如果有）
	editDate := 0
	if date, ok := msg.GetEditDate(); ok {
		editDate = date
	}

	// 使用新的缓存方法检查是否为编辑或重复
	isEdit, shouldProcess := p.messageCache.AddOrUpdate(peerID, msg.ID, editDate)

	if !shouldProcess {
		// 真正的重复消息（既不是新消息也不是编辑更新）
		p.ext.Log().Debug("消息重复，已跳过",
			zap.Int("message_id", msg.ID),
			zap.Int64("channel_id", peerID))
		return 0, 0, nil
	}

	if isEdit {
		// 这是一条编辑更新的消息，但通过 NewChannelMessage 事件收到
		// 正常情况下不应该发生，但为了健壮性记录一下
		p.ext.Log().Warn("通过新消息事件收到编辑消息",
			zap.Int("message_id", msg.ID),
			zap.Int64("channel_id", peerID))
	}

	// 检查是否是监听的频道（为 forward_target 添加例外）
	if len(p.config.Monitor.Channels) > 0 {
		if !contains(p.config.Monitor.Channels, peerID) && !(p.config.Monitor.Features.AutoRecloneForwards && peerID == p.config.Bot.ForwardTarget) {
			return 0, 0, nil
		}
	}

	// 打印调试日志
	p.ext.Log().Debug("处理新消息",
		zap.Int("id", msg.ID),
		zap.Int64("channel_id", peerID),
		zap.String("content", msg.Message))
	fmt.Printf("📨 收到新消息: ID=%d, 频道=%d, 内容=\"%.50s...\"\n", msg.ID, peerID, msg.Message)

	p.messageCount++

	// 调用通用的消息处理逻辑
	return p.processMessageContent(ctx, msg, peerID, false)
}

// handleEditMessage 处理编辑的消息，返回 (有效订阅数, 有效节点数, error)
func (p *MessageProcessor) handleEditMessage(ctx context.Context, msg *tg.Message, entities tg.Entities) (int, int, error) {
	peerID := getPeerID(msg.PeerID)

	// 获取编辑时间
	editDate := 0
	if date, ok := msg.GetEditDate(); ok {
		editDate = date
	}

	// 使用新的缓存方法检查是否为编辑或重复
	isEdit, shouldProcess := p.messageCache.AddOrUpdate(peerID, msg.ID, editDate)

	if !shouldProcess {
		// 编辑时间未更新，可能是重复的编辑事件
		p.ext.Log().Debug("编辑消息重复，已跳过",
			zap.Int("message_id", msg.ID),
			zap.Int64("channel_id", peerID),
			zap.Int("edit_date", editDate))
		return 0, 0, nil
	}

	// 检查是否是监听的频道（为 forward_target 添加例外）
	if len(p.config.Monitor.Channels) > 0 {
		if !contains(p.config.Monitor.Channels, peerID) && !(p.config.Monitor.Features.AutoRecloneForwards && peerID == p.config.Bot.ForwardTarget) {
			return 0, 0, nil
		}
	}

	// 打印调试日志
	editLabel := "首次编辑"
	if isEdit {
		editLabel = "再次编辑"
	}
	p.ext.Log().Debug("处理编辑消息",
		zap.Int("id", msg.ID),
		zap.Int64("channel_id", peerID),
		zap.Int("edit_date", editDate),
		zap.String("edit_type", editLabel),
		zap.String("content", msg.Message))
	fmt.Printf("📨 收到新消息[编辑]: ID=%d, 频道=%d, 内容=\"%.50s...\"\n",
		msg.ID, peerID, msg.Message)

	p.editedMsgCount++

	// 调用通用的消息处理逻辑
	return p.processMessageContent(ctx, msg, peerID, true)
}

// processMessageContent 处理消息内容的通用逻辑（用于新消息和编辑消息）
func (p *MessageProcessor) processMessageContent(ctx context.Context, msg *tg.Message, peerID int64, isEdited bool) (int, int, error) {
	msgType := "新消息"
	if isEdited {
		msgType = "编辑消息"
	}

	// 【新功能】检查是否为 forward_target 频道的转发消息，自动克隆去除转发头
	// 如果是 forward_target 频道，输出完整的原始消息结构
	// if peerID == p.config.Bot.ForwardTarget {
	// 	p.ext.Log().Info("📋 forward_target 频道收到消息",
	// 		zap.Int("message_id", msg.ID),
	// 		zap.Any("raw_message", msg))
	// }

	if p.config.Monitor.Features.AutoRecloneForwards && peerID == p.config.Bot.ForwardTarget {
		fwdInfo, hasFwdFrom := msg.GetFwdFrom()
		if hasFwdFrom {
			// 检查是否为消息集合（Media Group/Album）
			groupedID, hasGroupedID := msg.GetGroupedID()
			
			if hasGroupedID {
				// 这是消息集合的一部分，使用 groupedID 作为唯一标识来去重
				// 将 groupedID 转换为 int 类型用于缓存
				groupIDInt := int(groupedID)
				
				// 检查是否已处理过此消息集合
				if p.messageCache.Has(peerID, groupIDInt) {
					// 此消息集合已处理过，跳过
					p.ext.Log().Debug("消息集合已处理，跳过",
						zap.Int("message_id", msg.ID),
						zap.Int64("grouped_id", groupedID),
						zap.Int64("channel_id", peerID))
					return 0, 0, nil
				}
				
				// 标记此消息集合已处理（使用 groupedID 作为缓存键）
				p.messageCache.Add(peerID, groupIDInt, 0)
				
				p.ext.Log().Info("✅ 检测到转发消息集合，准备自动克隆",
					zap.Int("message_id", msg.ID),
					zap.Int64("grouped_id", groupedID),
					zap.Int64("channel_id", peerID),
					zap.String("info", "仅处理集合第一条消息"))
			} else {
				// 单条消息
				p.ext.Log().Info("✅ 检测到转发消息，准备自动克隆",
					zap.Int("message_id", msg.ID),
					zap.Int64("channel_id", peerID))
			}
			
			go func() {
				// 使用手动克隆模式（默认去除剧透效果）
				if err := p.recloneForwardedMessageManual(context.Background(), msg, peerID, fwdInfo); err != nil {
					p.ext.Log().Error("❌ 自动克隆转发消息失败",
						zap.Int("message_id", msg.ID),
						zap.Int64("channel_id", peerID),
						zap.Error(err))
				}
			}()
			// 继续正常处理消息（如果需要提取订阅链接等）
		}
	}

	// 获取消息文本
	text := msg.Message
	if text == "" {
		fmt.Printf("⏭️  %s跳过: 空消息 (ID=%d)\n", msgType, msg.ID)
		return 0, 0, nil
	}

	// 检查是否包含订阅格式或节点格式
	hasSubsFormat := matchAny(text, p.config.Monitor.Filters.Subs)
	hasNodeFormat := matchAny(text, p.config.Monitor.Filters.SS)

	if !hasSubsFormat && !hasNodeFormat {
		fmt.Printf("⏭️  %s跳过: 不包含订阅/节点格式 (ID=%d)\n", msgType, msg.ID)
		return 0, 0, nil // 既不是订阅也不是节点，跳过
	}

	// 白名单频道跳过二次过滤
	isWhitelisted := contains(p.config.Monitor.WhitelistChannels, peerID)

	// 仅对订阅格式进行二次内容过滤（节点格式不进行二次过滤）
	if hasSubsFormat && !hasNodeFormat {
		// 纯订阅格式，需要二次过滤
		if !isWhitelisted && len(p.config.Monitor.Filters.ContentFilter) > 0 {
			if !matchAny(text, p.config.Monitor.Filters.ContentFilter) {
				fmt.Printf("⏭️  %s跳过: 未通过内容二次过滤 (ID=%d)\n", msgType, msg.ID)
				return 0, 0, nil
			}
		}
	}
	// 如果是节点格式（hasNodeFormat为true），则跳过二次过滤

	// 提取链接
	links := p.ExtractAllLinks(text)
	if len(links) == 0 {
		fmt.Printf("⏭️  %s跳过: 未提取到有效链接 (ID=%d)\n", msgType, msg.ID)
		return 0, 0, nil
	}

	// 过滤黑名单链接
	filteredLinks := p.FilterLinks(links, p.config.Monitor.Filters.LinkBlacklist)
	if len(filteredLinks) == 0 {
		fmt.Printf("⏭️  %s跳过: 所有链接都在黑名单中 (ID=%d, 原始链接数=%d)\n", msgType, msg.ID, len(links))
		return 0, 0, nil
	}

	// 分组：订阅和节点
	var subscriptions []string
	var nodes []string

	for _, link := range filteredLinks {
		if p.IsProxyNode(link) {
			nodes = append(nodes, link)
		} else {
			subscriptions = append(subscriptions, link)
		}
	}

	subsCount := 0
	nodeCount := 0

	msgTypeLabel := "新消息"
	if isEdited {
		msgTypeLabel = "编辑消息"
	}

	fmt.Printf("🔗 %s提取到 %d 个有效链接，准备提交... (ID=%d)\n", msgTypeLabel, len(filteredLinks), msg.ID)
	p.ext.Log().Debug("准备发送链接到API",
		zap.Int("message_id", msg.ID),
		zap.String("type", msgTypeLabel),
		zap.Int("subscriptions_count", len(subscriptions)),
		zap.Int("nodes_count", len(nodes)))

	// 处理订阅（逐个调用addSubscription）
	for _, subLink := range subscriptions {
		p.ext.Log().Debug("调用addSubscription", zap.String("link", subLink))
		if err := p.addSubscription(subLink); err != nil {
			p.ext.Log().Info(fmt.Sprintf("%s-发送订阅失败", msgTypeLabel),
				zap.String("link", subLink),
				zap.Error(err))
		} else {
			subsCount++
			p.ext.Log().Info(fmt.Sprintf("%s-新订阅", msgTypeLabel),
				zap.Int64("channel", peerID),
				zap.String("link", subLink))

			emoji := "✅"
			if isEdited {
				emoji = "🔄"
			}
			fmt.Printf("%s %s-新订阅: %s (频道: %d)\n", emoji, msgTypeLabel, subLink, peerID)
		}
	}

	// 处理节点（批量汇总提交）
	if len(nodes) > 0 {
		p.ext.Log().Info(fmt.Sprintf("开始批量提交 %d 个节点", len(nodes)))

		if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
			p.ext.Log().Warn("订阅 API 未配置或未启用")
		} else {
			apiURL := p.config.Monitor.SubscriptionAPI.AddURL

			// 将多个节点用\n连接
			batchSS := strings.Join(nodes, "\n")

			type SubscriptionRequest struct {
				SubURL string `json:"sub_url,omitempty"`
				SS     string `json:"ss,omitempty"`
				Test   bool   `json:"test"`
			}

			reqBody := SubscriptionRequest{
				SS:   batchSS,
				Test: true,
			}

			jsonData, err := json.Marshal(reqBody)
			if err != nil {
				p.ext.Log().Debug("JSON 序列化失败", zap.Error(err))
			} else {
				req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
				if err != nil {
					p.ext.Log().Debug("创建请求失败", zap.Error(err))
				} else {
					req.Header.Set("X-API-Key", p.config.Monitor.SubscriptionAPI.ApiKey)
					req.Header.Set("Content-Type", "application/json")

					client := &http.Client{Timeout: 120 * time.Second}
					resp, err := client.Do(req)
					if err != nil {
						p.ext.Log().Debug("批量节点 API 请求失败", zap.Error(err))
					} else {
						defer resp.Body.Close()

						body, err := io.ReadAll(resp.Body)
						if err != nil {
							p.ext.Log().Debug("读取响应失败", zap.Error(err))
						} else {
							// 记录原始响应（用于调试）
							p.ext.Log().Debug("批量节点API 响应",
								zap.Int("status", resp.StatusCode),
								zap.String("body", string(body)))

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

							var response SubscriptionResponse
							if err := json.Unmarshal(body, &response); err != nil {
								p.ext.Log().Debug("批量节点响应解析失败",
									zap.Error(err),
									zap.String("body", string(body)),
									zap.Int("status", resp.StatusCode))
								// 如果是 200 状态码但解析失败，可能是纯文本响应，视为成功
								if resp.StatusCode == 200 {
									nodeCount = len(nodes)
									p.ext.Log().Debug(msgTypeLabel+"批量节点添加成功", zap.Int("node_count", len(nodes)))
								}
							} else {
								// 处理响应
								if resp.StatusCode == 200 {
									if response.TestedNodes != nil {
										// 检测模式响应 - 记录简洁日志
										p.ext.Log().Info(msgTypeLabel+"批量节点检测完成",
											zap.Int("node_count", len(nodes)),
											zap.Int("tested_nodes", *response.TestedNodes),
											zap.Intp("passed_nodes", response.PassedNodes),
											zap.Intp("failed_nodes", response.FailedNodes),
											zap.Intp("added_nodes", response.AddedNodes),
											zap.String("duration", response.Duration))
										nodeCount = len(nodes)
									} else {
										// 普通模式响应
										p.ext.Log().Info(msgTypeLabel+"批量节点添加成功",
											zap.Int("node_count", len(nodes)))
										nodeCount = len(nodes)
									}
									emoji := "✅"
									if isEdited {
										emoji = "🔄"
									}
									fmt.Printf("%s %s-批量节点: %d个 (频道: %d)\n", emoji, msgTypeLabel, len(nodes), peerID)
								} else if resp.StatusCode == 409 {
									p.ext.Log().Debug(msgTypeLabel+"批量节点已存在",
										zap.Int("node_count", len(nodes)))
									nodeCount = len(nodes)
									emoji := "⚠️"
									if isEdited {
										emoji = "🔄"
									}
									fmt.Printf("%s %s-批量节点已存在: %d个 (频道: %d)\n", emoji, msgTypeLabel, len(nodes), peerID)
								} else {
									errorMsg := response.Error
									if errorMsg == "" {
										errorMsg = response.Message
									}
									p.ext.Log().Debug(msgTypeLabel+"批量节点提交失败",
										zap.Int("node_count", len(nodes)),
										zap.String("error", errorMsg))
								}
							}
						}
					}
				}
			}
		}
	}

	// 输出处理结果摘要
	if subsCount > 0 || nodeCount > 0 {
		fmt.Printf("✅ %s处理完成: 有效订阅=%d, 有效节点=%d (ID=%d)\n", msgTypeLabel, subsCount, nodeCount, msg.ID)
	} else {
		fmt.Printf("⚠️  %s处理完成: 所有链接提交失败 (ID=%d, 尝试数=%d)\n", msgTypeLabel, msg.ID, len(filteredLinks))
	}

	return subsCount, nodeCount, nil
}

// addSubscription 添加订阅或单个节点
func (p *MessageProcessor) addSubscription(link string) error {
	p.ext.Log().Debug("进入addSubscription函数", zap.String("link", link))
	if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
		p.ext.Log().Warn("订阅 API 未配置或未启用",
			zap.Bool("enabled", p.config.Monitor.Enabled),
			zap.String("api_url", p.config.Monitor.SubscriptionAPI.AddURL))
		return fmt.Errorf("订阅 API 未配置")
	}

	// 使用配置文件中的完整 URL
	apiURL := p.config.Monitor.SubscriptionAPI.AddURL

	// 判断是订阅链接还是单个节点
	isNodeLink := p.IsProxyNode(link)

	// 构建请求体
	type SubscriptionRequest struct {
		SubURL string `json:"sub_url,omitempty"`
		SS     string `json:"ss,omitempty"`
		Test   bool   `json:"test"`
	}

	var reqBody SubscriptionRequest
	if isNodeLink {
		reqBody.SS = link
	} else {
		reqBody.SubURL = link
	}
	reqBody.Test = true

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
	client := &http.Client{Timeout: 120 * time.Second}
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

	var response SubscriptionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		p.ext.Log().Debug("解析响应失败",
			zap.Error(err),
			zap.String("body", string(body)),
			zap.Int("status", resp.StatusCode))

		// 如果是 200 状态码但解析失败，可能是纯文本响应，视为成功
		if resp.StatusCode == 200 {
			p.ext.Log().Debug(linkType+"添加成功（纯文本响应）", zap.String("link", link))
			return nil
		}
		return fmt.Errorf("解析响应失败 (状态码: %d): %w", resp.StatusCode, err)
	}

	// 处理响应
	if resp.StatusCode == 200 {
		// 检查是否为检测模式响应
		if response.TestedNodes != nil {
			// 检测模式响应 - 记录详细统计信息
			p.ext.Log().Info(linkType+"检测并添加成功",
				zap.String("link", link),
				zap.Int("tested_nodes", *response.TestedNodes),
				zap.Intp("passed_nodes", response.PassedNodes),
				zap.Intp("failed_nodes", response.FailedNodes),
				zap.Intp("added_nodes", response.AddedNodes),
				zap.String("duration", response.Duration),
				zap.Boolp("timeout", response.Timeout))
			if response.Timeout != nil && *response.Timeout {
				p.ext.Log().Warn(linkType+"检测超时", zap.String("warning", response.Warning))
			}
		} else {
			// 普通模式响应
			successMsg := response.Message
			if successMsg == "" {
				successMsg = linkType + "添加成功"
			}
			p.ext.Log().Info(linkType+"添加成功", zap.String("link", link), zap.String("message", successMsg))
		}
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
	fmt.Printf("📥 开始获取频道 %d 的历史消息（最多 %d 条）...\n", channelID, limit)

	// 保存频道名称
	var channelTitle string

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

		channelTitle = foundChannel.Title
		inputPeer.AccessHash = accessHash
	} else {
		// 成功获取频道信息
		switch chats := channel.(type) {
		case *tg.MessagesChats:
			if len(chats.Chats) > 0 {
				if ch, ok := chats.Chats[0].(*tg.Channel); ok {
					channelTitle = ch.Title
					inputPeer.AccessHash = ch.AccessHash
				}
			}
		}
	}

	// 获取历史消息（分页获取以突破100条限制）
	var allMessages []tg.MessageClass
	offsetID := 0
	batchSize := 100 // Telegram API 单次最多返回100条
	fetchedCount := 0

	for fetchedCount < limit {
		// 计算本次请求的数量
		requestLimit := batchSize
		if limit-fetchedCount < batchSize {
			requestLimit = limit - fetchedCount
		}

		history, err := p.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:       inputPeer,
			OffsetID:   offsetID,
			OffsetDate: 0,
			AddOffset:  0,
			Limit:      requestLimit,
			MaxID:      0,
			MinID:      0,
			Hash:       0,
		})

		if err != nil {
			return fmt.Errorf("获取历史消息失败: %w", err)
		}

		// 提取本批次的消息
		var batchMessages []tg.MessageClass
		switch h := history.(type) {
		case *tg.MessagesMessages:
			batchMessages = h.Messages
		case *tg.MessagesMessagesSlice:
			batchMessages = h.Messages
		case *tg.MessagesChannelMessages:
			batchMessages = h.Messages
		}

		// 如果没有更多消息，退出循环
		if len(batchMessages) == 0 {
			break
		}

		// 添加到总消息列表
		allMessages = append(allMessages, batchMessages...)
		fetchedCount += len(batchMessages)

		// 更新 offsetID 为最后一条消息的 ID
		if lastMsg, ok := batchMessages[len(batchMessages)-1].(*tg.Message); ok {
			offsetID = lastMsg.ID
		} else {
			break // 如果最后一条不是普通消息，退出
		}

		// 如果返回的消息数少于请求数，说明已经没有更多消息
		if len(batchMessages) < requestLimit {
			break
		}

		// 短暂延迟避免请求过快
		time.Sleep(100 * time.Millisecond)
	}

	messages := allMessages
	// fmt.Printf("✅ 实际获取到 %d 条历史消息\n", len(messages))

	// 处理每条消息，统计有效订阅和节点
	totalSubs := 0
	totalNodes := 0
	totalLinks := 0                           // 提取到的订阅/节点总数
	for i := len(messages) - 1; i >= 0; i-- { // 倒序处理，从旧到新
		msg, ok := messages[i].(*tg.Message)
		if !ok {
			continue
		}

		// 获取编辑时间（如果有）
		editDate := 0
		if date, ok := msg.GetEditDate(); ok {
			editDate = date
		}

		// 使用新的缓存方法进行去重检查
		_, shouldProcess := p.messageCache.AddOrUpdate(channelID, msg.ID, editDate)
		if !shouldProcess {
			continue // 如果已处理，则跳过
		}

		p.ext.Log().Debug("处理历史消息",
			zap.Int("message_id", msg.ID),
			zap.Int64("channel_id", channelID))

		// 统计提取的链接数（在处理之前）
		text := msg.Message
		if text != "" {
			// 检查是否包含订阅格式或节点格式
			hasSubsFormat := matchAny(text, p.config.Monitor.Filters.Subs)
			hasNodeFormat := matchAny(text, p.config.Monitor.Filters.SS)
			if hasSubsFormat || hasNodeFormat {
				links := p.ExtractAllLinks(text)
				if len(links) > 0 {
					filteredLinks := p.FilterLinks(links, p.config.Monitor.Filters.LinkBlacklist)
					totalLinks += len(filteredLinks)
				}
			}
		}

		// 直接调用 processMessageContent 处理历史消息（不需要重复去重检查）
		subsCount, nodeCount, _ := p.processMessageContent(ctx, msg, channelID, false)
		totalSubs += subsCount
		totalNodes += nodeCount
	}

	// 格式化输出统计信息
	fmt.Printf("✅ 频道名称: %s 频道ID:%d 历史消息:%d 订阅/节点数:%d 有效订阅:%d 有效节点:%d\n",
		channelTitle, channelID, len(messages), totalLinks, totalSubs, totalNodes)
	p.ext.Log().Info("历史消息处理完成",
		zap.String("频道名称", channelTitle),
		zap.Int64("频道ID", channelID),
		zap.Int("历史消息", len(messages)),
		zap.Int("订阅/节点数", totalLinks),
		zap.Int("有效订阅", totalSubs),
		zap.Int("有效节点", totalNodes))
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

// recloneForwardedMessage 克隆转发消息（去除转发头）
// getChannelAccessHash 获取频道的 AccessHash
func (p *MessageProcessor) getChannelAccessHash(ctx context.Context, channelID int64) (int64, error) {
	dialogs, err := p.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetDate: 0,
		OffsetID:   0,
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
		Hash:       0,
	})

	if err != nil {
		return 0, fmt.Errorf("获取对话列表失败: %w", err)
	}

	// 查找对应的频道
	var accessHash int64
	switch d := dialogs.(type) {
	case *tg.MessagesDialogs:
		for _, chat := range d.Chats {
			if ch, ok := chat.(*tg.Channel); ok && ch.ID == channelID {
				accessHash = ch.AccessHash
				break
			}
		}
	case *tg.MessagesDialogsSlice:
		for _, chat := range d.Chats {
			if ch, ok := chat.(*tg.Channel); ok && ch.ID == channelID {
				accessHash = ch.AccessHash
				break
			}
		}
	}

	if accessHash == 0 {
		return 0, fmt.Errorf("未找到频道 %d 的 AccessHash", channelID)
	}

	return accessHash, nil
}

// recloneForwardedMessageTDL 使用 tdl 库转发消息（已弃用，保留待测试后删除）
func (p *MessageProcessor) recloneForwardedMessageTDL(ctx context.Context, msg *tg.Message, channelID int64, fwdInfo tg.MessageFwdHeader) error {
	// 构造消息链接（私有频道格式）
	msgLink := fmt.Sprintf("https://t.me/c/%d/%d", channelID, msg.ID)
	
	p.ext.Log().Info("开始克隆转发消息",
		zap.Int("原消息ID", msg.ID),
		zap.Int64("频道ID", channelID),
		zap.String("消息链接", msgLink))
	
	// 使用现有的 forwardFromLink 方法，配置中的 forward_mode 已设为 clone
	// Single 设为 false 以提高批量转发效率
	if err := p.forwardFromLink(ctx, msgLink, &channelID, nil, false); err != nil {
		return fmt.Errorf("克隆转发失败: %w", err)
	}
	
	p.ext.Log().Info("✅ 克隆转发成功",
		zap.Int("原消息ID", msg.ID),
		zap.Int64("频道ID", channelID))
	
	// 克隆成功后删除原始带转发头的消息
	// 获取频道的 AccessHash
	accessHash, err := p.getChannelAccessHash(ctx, channelID)
	if err != nil {
		p.ext.Log().Warn("获取频道 AccessHash 失败（已成功克隆）",
			zap.Int("原消息ID", msg.ID),
			zap.Int64("频道ID", channelID),
			zap.Error(err))
		return nil // 不返回错误，因为克隆已经成功
	}
	
	// 使用 ChannelsDeleteMessages API 删除频道消息
	deleteRequest := &tg.ChannelsDeleteMessagesRequest{
		Channel: &tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: accessHash,
		},
		ID: []int{msg.ID},
	}
	
	affectedMessages, err := p.api.ChannelsDeleteMessages(ctx, deleteRequest)
	if err != nil {
		p.ext.Log().Warn("删除原始转发消息失败（已成功克隆）",
			zap.Int("原消息ID", msg.ID),
			zap.Int64("频道ID", channelID),
			zap.Error(err))
		// 不返回错误，因为克隆已经成功
	} else {
		p.ext.Log().Info("🗑️ 已删除原始转发消息",
			zap.Int("消息ID", msg.ID),
			zap.Int64("频道ID", channelID),
			zap.Int("pts", affectedMessages.Pts),
			zap.Int("count", affectedMessages.PtsCount))
	}
	
	return nil
}

// recloneForwardedMessageManual 手动克隆转发消息（默认去除转发头和剧透效果）
func (p *MessageProcessor) recloneForwardedMessageManual(ctx context.Context, msg *tg.Message, channelID int64, fwdInfo tg.MessageFwdHeader) error {
	p.ext.Log().Info("开始克隆转发消息（手动模式，去除剧透）",
		zap.Int("原消息ID", msg.ID),
		zap.Int64("频道ID", channelID))
	
	// 获取频道的 AccessHash
	accessHash, err := p.getChannelAccessHash(ctx, channelID)
	if err != nil {
		return fmt.Errorf("获取频道 AccessHash 失败: %w", err)
	}
	
	// 准备频道 InputPeer
	inputPeer := &tg.InputPeerChannel{
		ChannelID:  channelID,
		AccessHash: accessHash,
	}
	
	// 过滤消息实体，去除 Spoiler（文本剧透）- 无条件执行
	filteredEntities := p.filterSpoilerEntities(msg.Entities)
	
	// 构建发送请求
	var err error
	
	// 检查消息类型
	if media, ok := msg.GetMedia(); ok {
		// 包含媒体的消息（图片、视频等）- 去除媒体剧透
		_, err = p.api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
			Peer:     inputPeer,
			Media:    p.cloneInputMediaWithoutSpoiler(media), // 无条件去除剧透
			Message:  msg.Message,
			Entities: filteredEntities,
		})
	} else {
		// 纯文本消息
		_, err = p.api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
			Peer:     inputPeer,
			Message:  msg.Message,
			Entities: filteredEntities,
		})
	}
	
	if err != nil {
		return fmt.Errorf("重新发送消息失败: %w", err)
	}
	
	p.ext.Log().Info("✅ 克隆转发成功（已去除转发头和剧透）",
		zap.Int("原消息ID", msg.ID),
		zap.Int64("频道ID", channelID))
	
	// 删除原始带转发头的消息
	deleteRequest := &tg.ChannelsDeleteMessagesRequest{
		Channel: &tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: accessHash,
		},
		ID: []int{msg.ID},
	}
	
	affectedMessages, err := p.api.ChannelsDeleteMessages(ctx, deleteRequest)
	if err != nil {
		p.ext.Log().Warn("删除原始转发消息失败（已成功克隆）",
			zap.Int("原消息ID", msg.ID),
			zap.Int64("频道ID", channelID),
			zap.Error(err))
	} else {
		p.ext.Log().Info("🗑️ 已删除原始转发消息",
			zap.Int("消息ID", msg.ID),
			zap.Int64("频道ID", channelID),
			zap.Int("pts", affectedMessages.Pts),
			zap.Int("count", affectedMessages.PtsCount))
	}
	
	return nil
}

// filterSpoilerEntities 过滤掉 Spoiler 类型的实体（文本剧透）
func (p *MessageProcessor) filterSpoilerEntities(entities []tg.MessageEntityClass) []tg.MessageEntityClass {
	if len(entities) == 0 {
		return entities
	}
	
	filtered := make([]tg.MessageEntityClass, 0, len(entities))
	spoilerCount := 0
	
	for _, entity := range entities {
		// 检查是否为 Spoiler 类型
		if _, isSpoiler := entity.(*tg.MessageEntitySpoiler); isSpoiler {
			spoilerCount++
			continue // 跳过剧透实体
		}
		filtered = append(filtered, entity)
	}
	
	if spoilerCount > 0 {
		p.ext.Log().Info("已过滤文本剧透实体",
			zap.Int("过滤数量", spoilerCount),
			zap.Int("保留数量", len(filtered)))
	}
	
	return filtered
}

// cloneInputMediaWithoutSpoiler 克隆媒体对象为 InputMedia，并去除剧透标记
func (p *MessageProcessor) cloneInputMediaWithoutSpoiler(media tg.MessageMediaClass) tg.InputMediaClass {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		if photo, ok := m.Photo.(*tg.Photo); ok {
			// 检查原消息是否有剧透标记
			if m.Spoiler {
				p.ext.Log().Info("检测到图片剧透，已去除")
			}
			
			return &tg.InputMediaPhoto{
				ID: &tg.InputPhoto{
					ID:            photo.ID,
					AccessHash:    photo.AccessHash,
					FileReference: photo.FileReference,
				},
				Spoiler: false, // 强制设为 false，去除剧透效果
			}
		}
	case *tg.MessageMediaDocument:
		if doc, ok := m.Document.(*tg.Document); ok {
			// 检查原消息是否有剧透标记
			if m.Spoiler {
				p.ext.Log().Info("检测到视频/文档剧透，已去除")
			}
			
			return &tg.InputMediaDocument{
				ID: &tg.InputDocument{
					ID:            doc.ID,
					AccessHash:    doc.AccessHash,
					FileReference: doc.FileReference,
				},
				Spoiler: false, // 强制设为 false，去除剧透效果
			}
		}
	}
	
	// 不支持的媒体类型，返回空
	// 不支持的媒体类型，返回空
	p.ext.Log().Warn("不支持的媒体类型，将仅发送文本",
		zap.String("type", fmt.Sprintf("%T", media)))
	return &tg.InputMediaEmpty{}
}

