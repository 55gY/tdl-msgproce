// tdl-msgproce - 消息监听和处理核心逻辑
//
// 日志输出规范：
// - 使用 fmt.Printf() 输出用户可见的日志信息
// - 调试日志使用 // fmt.Printf() 注释格式
// - 不使用 zap 日志库
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
)

// SubscriptionResponse 订阅API响应结构
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
		// fmt.Printf("[DEBUG] 消息重复，已跳过 (message_id=%d, channel_id=%d)\n", msg.ID, peerID)
		return 0, 0, nil
	}

	if isEdit {
		// 这是一条编辑更新的消息，但通过 NewChannelMessage 事件收到
		// 正常情况下不应该发生，但为了健壮性记录一下
		fmt.Printf("⚠️  通过新消息事件收到编辑消息 (message_id=%d, channel_id=%d)\n", msg.ID, peerID)
	}

	// 检查是否是监听的频道（为 forward_target 添加例外）
	if len(p.config.Monitor.Channels) > 0 {
		if !contains(p.config.Monitor.Channels, peerID) && !(p.config.Monitor.Features.AutoRecloneForwards && peerID == p.config.Bot.ForwardTarget) {
			return 0, 0, nil
		}
	}

	// 打印调试日志
	// fmt.Printf("[DEBUG] 处理新消息 (id=%d, channel_id=%d, content=%.50s)\n", msg.ID, peerID, msg.Message)
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
		// fmt.Printf("[DEBUG] 编辑消息重复，已跳过 (message_id=%d, channel_id=%d, edit_date=%d)\n", msg.ID, peerID, editDate)
		return 0, 0, nil
	}

	// 检查是否是监听的频道（为 forward_target 添加例外）
	if len(p.config.Monitor.Channels) > 0 {
		if !contains(p.config.Monitor.Channels, peerID) && !(p.config.Monitor.Features.AutoRecloneForwards && peerID == p.config.Bot.ForwardTarget) {
			return 0, 0, nil
		}
	}

	// 打印调试日志
	_ = isEdit
	// fmt.Printf("[DEBUG] 处理编辑消息 (id=%d, channel_id=%d, edit_date=%d, content=%.50s)\n", msg.ID, peerID, editDate, msg.Message)
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
	// fmt.Printf("📋 forward_target 频道收到消息 (message_id=%d): %+v\n", msg.ID, msg)

	if p.config.Monitor.Features.AutoRecloneForwards && peerID == p.config.Bot.ForwardTarget {
		fwdInfo, hasFwdFrom := msg.GetFwdFrom()
		if hasFwdFrom {
			// 检查是否为消息集合（Media Group/Album）
			groupedID, hasGroupedID := msg.GetGroupedID()

			if hasGroupedID {
				// 这是消息集合的一部分
				// 使用 groupedID 作为唯一标识
				groupIDInt := int(groupedID)

				// 收集消息ID到集合中
				p.groupedMessagesMu.Lock()
				p.groupedMessages[groupedID] = append(p.groupedMessages[groupedID], msg.ID)
				p.groupedMessagesMu.Unlock()

				// 检查是否已经在处理队列中
				if p.messageCache.Has(peerID, groupIDInt) {
					// 此消息集合已标记处理，跳过
					// fmt.Printf("[DEBUG] 消息集合已在处理队列，跳过 (message_id=%d, grouped_id=%d, channel_id=%d)\n", msg.ID, groupedID, peerID)
					return 0, 0, nil
				}

				// 标记此消息集合为处理中
				p.messageCache.Add(peerID, groupIDInt, 0)

				fmt.Printf("✅ 检测到转发消息集合 (message_id=%d, grouped_id=%d, channel_id=%d, 延迟2秒后处理)\n", msg.ID, groupedID, peerID)

				// 延迟处理：等待消息集合的所有消息到达
				go func(capturedGroupID int64, capturedChannelID int64, capturedMsg *tg.Message, capturedFwdInfo tg.MessageFwdHeader) {
					// 等待2秒，确保集合中的所有消息都已到达
					time.Sleep(2 * time.Second)

					// 获取收集到的所有消息ID
					p.groupedMessagesMu.Lock()
					allMessageIDs := p.groupedMessages[capturedGroupID]
					delete(p.groupedMessages, capturedGroupID) // 清理
					p.groupedMessagesMu.Unlock()

					fmt.Printf("开始处理消息集合 (first_message_id=%d, grouped_id=%d, total_messages=%d)\n", capturedMsg.ID, capturedGroupID, len(allMessageIDs))

					if err := p.recloneForwardedMessageGroup(context.Background(), capturedMsg, capturedChannelID, capturedFwdInfo, allMessageIDs); err != nil {
						fmt.Printf("❌ 自动克隆转发消息失败 (message_id=%d, grouped_id=%d, channel_id=%d): %v\n", capturedMsg.ID, capturedGroupID, capturedChannelID, err)
					}
				}(groupedID, peerID, msg, fwdInfo)
				// 跳过后续处理（不提取订阅链接等）
				return 0, 0, nil
			} else {
				// 单条消息，立即处理
				fmt.Printf("✅ 检测到转发消息，准备自动克隆 (message_id=%d, channel_id=%d)\n", msg.ID, peerID)

				go func() {
					if err := p.recloneForwardedMessage(context.Background(), msg, peerID, fwdInfo); err != nil {
						fmt.Printf("❌ 自动克隆转发消息失败 (message_id=%d, channel_id=%d): %v\n", msg.ID, peerID, err)
					}
				}()
			}
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
	// fmt.Printf("[DEBUG] 准备发送链接到API (message_id=%d, type=%s, subscriptions_count=%d, nodes_count=%d)\n", msg.ID, msgTypeLabel, len(subscriptions), len(nodes))

	// 处理订阅（逐个调用addSubscription）
	for _, subLink := range subscriptions {
		// fmt.Printf("[DEBUG] 调用addSubscription (link=%s)\n", subLink)
		if err := p.addSubscription(subLink); err != nil {
			fmt.Printf("%s-发送订阅失败 (link=%s): %v\n", msgTypeLabel, subLink, err)
		} else {
			subsCount++
			// fmt.Printf("[DEBUG] %s-新订阅 (channel=%d, link=%s)\n", msgTypeLabel, peerID, subLink)

			emoji := "✅"
			if isEdited {
				emoji = "🔄"
			}
			fmt.Printf("%s %s-新订阅: %s (频道: %d)\n", emoji, msgTypeLabel, subLink, peerID)
		}
	}

	// 处理节点（批量汇总提交）
	if len(nodes) > 0 {
		// fmt.Printf("[DEBUG] 开始批量提交 %d 个节点\n", len(nodes))

		if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
			fmt.Printf("⚠️  订阅 API 未配置或未启用\n")
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
				// fmt.Printf("[DEBUG] JSON 序列化失败: %v\n", err)
			} else {
				req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
				if err != nil {
					// fmt.Printf("[DEBUG] 创建请求失败: %v\n", err)
				} else {
					req.Header.Set("X-API-Key", p.config.Monitor.SubscriptionAPI.ApiKey)
					req.Header.Set("Content-Type", "application/json")

					client := &http.Client{Timeout: 120 * time.Second}
					resp, err := client.Do(req)
					if err != nil {
						// fmt.Printf("[DEBUG] 批量节点 API 请求失败: %v\n", err)
					} else {
						defer resp.Body.Close()

						body, err := io.ReadAll(resp.Body)
						if err != nil {
							// fmt.Printf("[DEBUG] 读取响应失败: %v\n", err)
						} else {
							// 记录原始响应（用于调试）
							// fmt.Printf("[DEBUG] 批量节点API 响应 (status=%d, body=%s)\n", resp.StatusCode, string(body))

							var response SubscriptionResponse
							if err := json.Unmarshal(body, &response); err != nil {
								// fmt.Printf("[DEBUG] 批量节点响应解析失败 (error=%v, body=%s, status=%d)\n", err, string(body), resp.StatusCode)
								// 如果是 200 状态码但解析失败，可能是纯文本响应，视为成功
								if resp.StatusCode == 200 {
									nodeCount = len(nodes)
									// fmt.Printf("[DEBUG] %s批量节点添加成功 (node_count=%d)\n", msgTypeLabel, len(nodes))
								}
							} else {
								// 处理响应
								if resp.StatusCode == 200 {
									if response.TestedNodes != nil {
										// 检测模式响应 - 记录简洁日志
										fmt.Printf("✅ %s批量节点检测完成 (node_count=%d, tested=%d, passed=%v, failed=%v, added=%v, duration=%s)\n",
											msgTypeLabel, len(nodes), *response.TestedNodes, response.PassedNodes, response.FailedNodes, response.AddedNodes, response.Duration)
										nodeCount = len(nodes)
									} else {
										// 普通模式响应
										fmt.Printf("✅ %s批量节点添加成功 (node_count=%d)\n", msgTypeLabel, len(nodes))
										nodeCount = len(nodes)
									}
									emoji := "✅"
									if isEdited {
										emoji = "🔄"
									}
									fmt.Printf("%s %s-批量节点: %d个 (频道: %d)\n", emoji, msgTypeLabel, len(nodes), peerID)
								} else if resp.StatusCode == 409 {
									// fmt.Printf("[DEBUG] %s批量节点已存在 (node_count=%d)\n", msgTypeLabel, len(nodes))
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
									// fmt.Printf("[DEBUG] %s批量节点提交失败 (node_count=%d, error=%s)\n", msgTypeLabel, len(nodes), errorMsg)
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
	// fmt.Printf("[DEBUG] 进入addSubscription函数 (link=%s)\n", link)
	if !p.config.Monitor.Enabled || p.config.Monitor.SubscriptionAPI.AddURL == "" {
		fmt.Printf("⚠️  订阅 API 未配置或未启用 (enabled=%v, api_url=%s)\n", p.config.Monitor.Enabled, p.config.Monitor.SubscriptionAPI.AddURL)
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
	// fmt.Printf("[DEBUG] API 响应 (type=%s, status=%d, body=%s)\n", linkType, resp.StatusCode, string(body))

	// 解析响应
	var response SubscriptionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		// fmt.Printf("[DEBUG] 解析响应失败 (error=%v, body=%s, status=%d)\n", err, string(body), resp.StatusCode)

		// 如果是 200 状态码但解析失败，可能是纯文本响应，视为成功
		if resp.StatusCode == 200 {
			// fmt.Printf("[DEBUG] %s添加成功（纯文本响应） (link=%s)\n", linkType, link)
			return nil
		}
		return fmt.Errorf("解析响应失败 (状态码: %d): %w", resp.StatusCode, err)
	}

	// 处理响应
	if resp.StatusCode == 200 {
		// 检查是否为检测模式响应
		if response.TestedNodes != nil {
			// 检测模式响应 - 记录详细统计信息
			fmt.Printf("✅ %s检测并添加成功 (link=%s, tested=%d, passed=%v, failed=%v, added=%v, duration=%s, timeout=%v)\n",
				linkType, link, *response.TestedNodes, response.PassedNodes, response.FailedNodes, response.AddedNodes, response.Duration, response.Timeout)
			if response.Timeout != nil && *response.Timeout {
				fmt.Printf("⚠️  %s检测超时 (warning=%s)\n", linkType, response.Warning)
			}
		} else {
			// 普通模式响应
			successMsg := response.Message
			if successMsg == "" {
				successMsg = linkType + "添加成功"
			}
			fmt.Printf("✅ %s添加成功 (link=%s, message=%s)\n", linkType, link, successMsg)
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
		// fmt.Printf("[DEBUG] %s已存在 (link=%s)\n", linkType, link)
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

		// fmt.Printf("[DEBUG] 处理历史消息 (message_id=%d, channel_id=%d)\n", msg.ID, channelID)

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
	// fmt.Printf("[DEBUG] 历史消息处理完成 (频道名称=%s, 频道ID=%d, 历史消息=%d, 订阅/节点数=%d, 有效订阅=%d, 有效节点=%d)\n",
	// channelTitle, channelID, len(messages), totalLinks, totalSubs, totalNodes)
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

// recloneForwardedMessageGroup 克隆转发消息集合（去除转发头）并删除所有原始消息
func (p *MessageProcessor) recloneForwardedMessageGroup(ctx context.Context, msg *tg.Message, channelID int64, fwdInfo tg.MessageFwdHeader, messageIDs []int) error {
	// 构造消息链接（私有频道格式）
	msgLink := fmt.Sprintf("https://t.me/c/%d/%d", channelID, msg.ID)

	fmt.Printf("✅ 开始克隆转发消息集合 (原消息ID=%d, 频道ID=%d, 消息链接=%s, 消息数量=%d)\n", msg.ID, channelID, msgLink, len(messageIDs))

	// 使用现有的 forwardFromLink 方法，Single 设为 false：让 tdl 自动检测消息集合并作为专辑转发
	if err := p.forwardFromLink(ctx, msgLink, &channelID, nil, false, msg); err != nil {
		return fmt.Errorf("克隆转发失败: %w", err)
	}

	fmt.Printf("✅ 克隆转发成功 (原消息ID=%d, 频道ID=%d)\n", msg.ID, channelID)

	// 克隆成功后删除所有原始带转发头的消息
	accessHash, err := p.getChannelAccessHash(ctx, channelID)
	if err != nil {
		fmt.Printf("⚠️  获取频道 AccessHash 失败（已成功克隆） (原消息IDs=%v, 频道ID=%d): %v\n", messageIDs, channelID, err)
		return nil
	}

	// 使用 ChannelsDeleteMessages API 删除所有消息
	deleteRequest := &tg.ChannelsDeleteMessagesRequest{
		Channel: &tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: accessHash,
		},
		ID: messageIDs, // 删除所有消息
	}

	affectedMessages, err := p.api.ChannelsDeleteMessages(ctx, deleteRequest)
	if err != nil {
		fmt.Printf("⚠️  删除原始转发消息集合失败（已成功克隆） (原消息IDs=%v, 频道ID=%d): %v\n", messageIDs, channelID, err)
	} else {
		fmt.Printf("🗑️ 已删除原始转发消息集合 (消息IDs=%v, 频道ID=%d, 数量=%d, pts=%d, count=%d)\n",
			messageIDs, channelID, len(messageIDs), affectedMessages.Pts, affectedMessages.PtsCount)
	}

	return nil
}

func (p *MessageProcessor) recloneForwardedMessage(ctx context.Context, msg *tg.Message, channelID int64, fwdInfo tg.MessageFwdHeader) error {
	// 构造消息链接（私有频道格式）
	msgLink := fmt.Sprintf("https://t.me/c/%d/%d", channelID, msg.ID)

	fmt.Printf("✅ 开始克隆转发消息 (原消息ID=%d, 频道ID=%d, 消息链接=%s)\n", msg.ID, channelID, msgLink)

	// 使用现有的 forwardFromLink 方法，配置中的 forward_mode 已设为 clone
	// Single 设为 false 以提高批量转发效率
	if err := p.forwardFromLink(ctx, msgLink, &channelID, nil, false, msg); err != nil {
		return fmt.Errorf("克隆转发失败: %w", err)
	}

	fmt.Printf("✅ 克隆转发成功 (原消息ID=%d, 频道ID=%d)\n", msg.ID, channelID)

	// 克隆成功后删除原始带转发头的消息
	// 获取频道的 AccessHash
	accessHash, err := p.getChannelAccessHash(ctx, channelID)
	if err != nil {
		fmt.Printf("⚠️  获取频道 AccessHash 失败（已成功克隆） (原消息ID=%d, 频道ID=%d): %v\n", msg.ID, channelID, err)
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
		fmt.Printf("⚠️  删除原始转发消息失败（已成功克隆） (原消息ID=%d, 频道ID=%d): %v\n", msg.ID, channelID, err)
		// 不返回错误，因为克隆已经成功
	} else {
		fmt.Printf("🗑️ 已删除原始转发消息 (消息ID=%d, 频道ID=%d, pts=%d, count=%d)\n",
			msg.ID, channelID, affectedMessages.Pts, affectedMessages.PtsCount)
	}

	return nil
}
