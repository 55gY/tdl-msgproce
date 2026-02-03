// tdl-msgproce - Telegram 消息处理扩展
// 
// 日志输出规范：
// - 使用 fmt.Printf() 输出用户可见的日志信息
// - 调试日志使用 // fmt.Printf() 注释格式
// - 不使用 zap 日志库
package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gotd/td/tg"
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
		fmt.Printf("❌ 配置加载失败: %v\n", err)
		return err
	}
	fmt.Printf("✅ 配置加载成功\n")

	// 4. 从 ext 对象中获取由 tdl 框架为我们创建好的、功能完整的客户端
	client := ext.Client()
	api := client.API()

	// 获取当前用户信息
	self, err := getSelfUser(ctx, api)
	if err != nil {
		return err
	}
	fmt.Printf("👤 TDL 用户: %s (ID: %d)\n", self.FirstName, self.ID)

	// 创建处理器，并将功能完整的 client 传递进去
	processor := &MessageProcessor{
		ext:             ext,
		config:          config,
		api:             api,
		client:          client, // 使用 tdl 为我们创建好的客户端
		selfUserID:      self.ID,
		messageCache:    NewMessageCache(20000),
		channelPts:      make(map[int64]int), // 初始化 pts 状态
		linkRegex:       buildLinkRegex(config), // 预编译链接提取正则
		groupedMessages: make(map[int64][]int), // 初始化消息集合追踪
	}

	// 5. 调用新方法，将所有的消息处理逻辑注册到 dispatcher 中
	processor.RegisterHandlers(dispatcher)

	// 启动后台服务
	errChan := make(chan error, 4)
	activeServices := 0

	if config.Monitor.Enabled {
		fmt.Printf("👂 启动频道消息监听器...\n")
		activeServices++
		go func() {
			errChan <- processor.StartMessageListener(ctx)
		}()
	}

	if config.Bot.Enabled {
		fmt.Printf("🤖 启动 Telegram Bot...\n")
		activeServices++
		go func() {
			errChan <- processor.StartTelegramBot(ctx)
		}()
	}

	if config.Proxy.Enabled {
		fmt.Printf("🔄 启动 HTTP 代理服务... (地址: %s:%d)\n", config.Proxy.Host, config.Proxy.Port)
		activeServices++
		proxyServer := NewProxyServer(&config.Proxy)
		go func() {
			errChan <- proxyServer.Start(ctx)
		}()
	}

	if config.CheckIn.Enabled && len(config.CheckIn.Tasks) > 0 {
		fmt.Printf("🕐 启动定时签到服务... (任务数: %d)\n", len(config.CheckIn.Tasks))
		activeServices++
		go func() {
			errChan <- processor.StartCheckInScheduler(ctx)
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
		fmt.Printf("收到停止信号，正在关闭...\n")
		return nil
	}

	// 等待服务出错或程序退出
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		fmt.Printf("收到停止信号，正在关闭...\n")
		return nil
	}
}
