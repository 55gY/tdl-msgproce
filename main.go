package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"github.com/iyear/tdl/extension"
)

func main() {
	// 1. 创建 dispatcher，它将作为所有更新事件的路由器
	dispatcher := tg.NewUpdateDispatcher()

	// 2. 将 dispatcher 作为 UpdateHandler 传入 extension.New。
	// tdl 的扩展框架会自动为我们创建一个配置了此 handler 的 gotd 客户端。
	extension.New(extension.Options{
		UpdateHandler: dispatcher,
	})(func(ctx context.Context, ext *extension.Extension) error {
		// 3. 将 dispatcher 传入我们自己的 run 函数
		return run(ctx, ext, dispatcher)
	})
}

func run(ctx context.Context, ext *extension.Extension, dispatcher tg.UpdateDispatcher) error {
	// 启动信息
	fmt.Println("========================================")
	fmt.Println("🚀 tdl-msgproce 扩展启动中 (v2, 已重构)...")
	fmt.Printf("📂 数据目录: %s\n", ext.Config().DataDir)

	// 加载配置
	config, err := loadConfig(filepath.Join(ext.Config().DataDir, "config.yaml"))
	if err != nil {
		ext.Log().Info("配置加载失败", zap.Error(err))
		return err
	}
	ext.Log().Info("✅ 配置加载成功")

	// 4. 从 ext 对象中获取由 tdl 框架为我们创建好的、功能完整的客户端
	client := ext.Client()
	api := client.API()

	// 获取当前用户信息
	self, err := getSelfUser(ctx, api)
	if err != nil {
		return err
	}
	ext.Log().Info("👤 TDL 用户", zap.String("name", self.FirstName), zap.Int64("id", self.ID))

	// 创建处理器，并将功能完整的 client 传递进去
	processor := &MessageProcessor{
		ext:             ext,
		config:          config,
		api:             api,
		client:          client, // 使用 tdl 为我们创建好的客户端
		selfUserID:      self.ID,
		messageCache:    NewMessageCache(20000),
		channelPts:      make(map[int64]int), // 初始化 pts 状态
		linkRegex:       buildLinkRegex(config, ext.Log()), // 预编译链接提取正则
		groupedMessages: make(map[int64][]int), // 初始化消息集合追踪
	}

	// 5. 调用新方法，将所有的消息处理逻辑注册到 dispatcher 中
	processor.RegisterHandlers(dispatcher)

	// 启动后台服务
	errChan := make(chan error, 2)
	activeServices := 0

	if config.Monitor.Enabled {
		ext.Log().Info("👂 启动频道消息监听器...")
		activeServices++
		go func() {
			errChan <- processor.StartMessageListener(ctx)
		}()
	}

	if config.Bot.Enabled {
		ext.Log().Info("🤖 启动 Telegram Bot...")
		activeServices++
		go func() {
			errChan <- processor.StartTelegramBot(ctx)
		}()
	}

	if config.Proxy.Enabled {
		ext.Log().Info("🔄 启动 HTTP 代理服务...", zap.String("addr", fmt.Sprintf("%s:%d", config.Proxy.Host, config.Proxy.Port)))
		activeServices++
		proxyServer := NewProxyServer(&config.Proxy)
		go func() {
			errChan <- proxyServer.Start(ctx)
		}()
	}

	fmt.Println("========================================")
	if activeServices > 0 {
		fmt.Printf("✅ %d 个服务已启动\n", activeServices)
		fmt.Println("⏳ 运行中... (按 Ctrl+C 退出)")
	} else {
		fmt.Println("⚠️  所有功能已禁用，处于待机状态")
		fmt.Println("⏳ 按 Ctrl+C 退出")
	}
	fmt.Println("========================================")

	if activeServices == 0 {
		<-ctx.Done()
		ext.Log().Info("收到停止信号，正在关闭...")
		return nil
	}

	// 等待服务出错或程序退出
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		ext.Log().Info("收到停止信号，正在关闭...")
		return nil
	}
}
