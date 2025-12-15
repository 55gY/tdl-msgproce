package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"

	"github.com/iyear/tdl/extension"
)

// MessageProcessor 消息处理器
type MessageProcessor struct {
	ext           *extension.Extension
	config        *Config
	api           *tg.Client
	selfUserID    int64
	messageCount  int64
	forwardCount  int64
	lastHeartbeat time.Time
}

// getSelfUser 获取当前用户信息
func getSelfUser(ctx context.Context, api *tg.Client) (*tg.User, error) {
	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("未获取到用户信息")
	}
	user, ok := users[0].(*tg.User)
	if !ok {
		return nil, fmt.Errorf("用户信息类型错误")
	}
	return user, nil
}

// StartHeartbeat 启动心跳
func (p *MessageProcessor) StartHeartbeat(ctx context.Context) {
	p.lastHeartbeat = time.Now()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// uptime := time.Since(p.lastHeartbeat).Round(time.Second)
			// msg := fmt.Sprintf("💓 运行: %v | 消息: %d | 转发: %d",
			// 	uptime, p.messageCount, p.forwardCount)
			// 为避免日志文件膨胀，默认不再将心跳写入日志或 stdout。
			// 如需输出，请在这里恢复 fmt.Println 和 p.ext.Log().Info。
			// fmt.Println(msg)
			// p.ext.Log().Info(msg)
			p.lastHeartbeat = time.Now()
		}
	}
}

// StartMessageListener 启动消息监听器
func (p *MessageProcessor) StartMessageListener(ctx context.Context) error {
	p.ext.Log().Info("消息监听器已启动")

	// 创建 dispatcher
	dispatcher := tg.NewUpdateDispatcher()

	// 处理新消息（包括群组和频道）
	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			return p.handleMessage(ctx, msg, e)
		}
		return nil
	})

	// 处理新频道消息（作为补充）
	dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			return p.handleMessage(ctx, msg, e)
		}
		return nil
	})

	// 处理编辑的消息
	dispatcher.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateEditChannelMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			return p.handleMessage(ctx, msg, e)
		}
		return nil
	})

	// 获取历史消息（如果启用，>0 则开启）
	fetchCount := p.config.Monitor.Features.FetchHistoryCount
	if fetchCount > 0 && len(p.config.Monitor.Channels) > 0 {
		p.ext.Log().Info(fmt.Sprintf("开始获取历史消息（每个频道 %d 条）...", fetchCount))
		fmt.Printf("📜 开始获取历史消息（每个频道 %d 条）...\n", fetchCount)

		for _, channelID := range p.config.Monitor.Channels {
			if err := p.fetchChannelHistory(ctx, channelID, fetchCount); err != nil {
				p.ext.Log().Warn(fmt.Sprintf("获取频道 %d 历史消息失败: %v", channelID, err))
				fmt.Printf("⚠️ 获取频道 %d 历史消息失败: %v\n", channelID, err)
			}
		}

		p.ext.Log().Info("历史消息获取完成")
		fmt.Println("✅ 历史消息获取完成")
	}

	// 创建更新处理器
	updateHandler := telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error {
		return dispatcher.Handle(ctx, u)
	})

	// 启动更新监听
	gaps := updates.New(updates.Config{
		Handler: updateHandler,
	})

	client := p.ext.Client()

	return client.Run(ctx, func(ctx context.Context) error {
		return gaps.Run(ctx, client.API(), p.selfUserID, updates.AuthOptions{
			OnStart: func(ctx context.Context) {
				p.ext.Log().Info("✅ 消息监听器启动成功")
			},
		})
	})
}
