package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"github.com/iyear/tdl/extension"
)

// MessageProcessor 消息处理器
type MessageProcessor struct {
	ext            *extension.Extension
	config         *Config
	api            *tg.Client
	client         *telegram.Client
	selfUserID     int64
	messageCount   int64
	editedMsgCount int64 // 编辑消息计数
	forwardCount   int64
	lastHeartbeat  time.Time
	messageCache   *MessageCache
	channelPts     map[int64]int // 每个频道的 pts 状态
	channelPtsMu   sync.RWMutex  // pts 状态的互斥锁
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
			p.lastHeartbeat = time.Now()
		}
	}
}

// RegisterHandlers 将所有消息处理逻辑注册到 dispatcher
func (p *MessageProcessor) RegisterHandlers(dispatcher tg.UpdateDispatcher) {
	// 1. 处理新的频道消息
	dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			if _, _, err := p.handleMessage(ctx, msg, e); err != nil {
				p.ext.Log().Info("处理新消息失败", zap.Error(err))
			}
		}
		return nil
	})

	// 2. 处理被编辑的频道消息
	dispatcher.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateEditChannelMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			if _, _, err := p.handleEditMessage(ctx, msg, e); err != nil {
				p.ext.Log().Info("处理编辑消息失败", zap.Error(err))
			}
		}
		return nil
	})
}

// StartMessageListener 启动消息监听器
func (p *MessageProcessor) StartMessageListener(ctx context.Context) error {
	// 异步获取历史消息，避免阻塞启动
	go func() {
		fetchCount := p.config.Monitor.Features.FetchHistoryCount
		if fetchCount > 0 && len(p.config.Monitor.Channels) > 0 {
			fmt.Printf("📥 历史消息功能: ✅ 已启用 (每个频道获取 %d 条)\n", fetchCount)
			fmt.Printf("🔄 正在获取 %d 个频道的历史消息...\n", len(p.config.Monitor.Channels))
			p.ext.Log().Info("开始获取历史消息", zap.Int("fetch_count", fetchCount))
			// 使用一个新的后台 context，以防主 context 因为其他原因提前结束
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			for _, channelID := range p.config.Monitor.Channels {
				if err := p.fetchChannelHistory(bgCtx, channelID, fetchCount); err != nil {
					p.ext.Log().Info("获取历史消息失败", zap.Int64("channel", channelID), zap.Error(err))
				}
			}
			fmt.Printf("✅ 历史消息获取完成\n")
			p.ext.Log().Info("历史消息获取完成")
		} else {
			fmt.Printf("📥 历史消息功能: ❌ 已禁用\n")
		}
	}()

	// client.Run 是一个阻塞操作。
	// tdl 框架已经为我们创建并配置好了这个 client，我们只需要调用 Run() 即可。
	// 它会自动处理连接、认证和接收更新的循环。
	// 当传入的 ctx 被取消时（例如用户按 Ctrl+C），Run 方法会自动返回。
	return p.client.Run(ctx, func(ctx context.Context) error {
		p.ext.Log().Info("✅ 消息监听器已连接并成功运行")
		<-ctx.Done()
		return ctx.Err()
	})
}
