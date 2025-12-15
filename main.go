package main

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/iyear/tdl/extension"
)

func main() {
	extension.New(extension.Options{})(run)
}

func run(ctx context.Context, ext *extension.Extension) error {
	// 启动信息
	fmt.Println("========================================")
	fmt.Println("🚀 tdl-msgproce 扩展启动中...")
	fmt.Printf("📂 数据目录: %s\n", ext.Config().DataDir)

	// 加载配置
	configPath := ext.Config().DataDir + "/config.yaml"
	fmt.Printf("📄 配置文件: %s\n", configPath)

	config, err := loadConfig(configPath)
	if err != nil {
		ext.Log().Error("配置加载失败", zap.Error(err))
		fmt.Printf("❌ 配置加载失败: %v\n", err)
		return fmt.Errorf("配置加载失败: %w", err)
	}

	fmt.Println("✅ 配置加载成功")

	// 显示功能状态
	activeFeatures := 0
	if config.Monitor.Enabled {
		fmt.Printf("📝 消息监听: 已启用 (%d 个频道)\n", len(config.Monitor.Channels))
		activeFeatures++
	} else {
		fmt.Println("📝 消息监听: 已禁用")
	}

	if config.Bot.Enabled {
		fmt.Printf("🤖 Bot 功能: 已启用\n")
		activeFeatures++
	} else {
		fmt.Println("🤖 Bot 功能: 已禁用")
	}

	if activeFeatures == 0 {
		fmt.Println("")
		fmt.Println("⚠️  当前没有启用任何功能，扩展将处于待机状态")
		fmt.Println("💡 请完成配置文件后重启服务")
	}

	ext.Log().Info("✅ 配置加载成功")

	// 获取 API 客户端
	api := ext.Client().API()

	// 获取当前用户信息
	self, err := getSelfUser(ctx, api)
	if err != nil {
		return fmt.Errorf("获取用户信息失败: %w", err)
	}

	fmt.Printf("👤 TDL 用户: %s %s (ID: %d)\n", self.FirstName, self.LastName, self.ID)
	ext.Log().Info(fmt.Sprintf("👤 TDL 用户: %s %s (ID: %d)", self.FirstName, self.LastName, self.ID))

	// 创建处理器
	processor := &MessageProcessor{
		ext:          ext,
		config:       config,
		api:          ext.Client().API(),
		messageCache: make(map[int]struct{}), // 初始化缓存
	}

	// 启动心跳
	go processor.StartHeartbeat(ctx)

	// 启动多个协程处理不同任务
	errChan := make(chan error, 2)
	activeServices := 0

	// 1. 启动消息监听器（监听频道，发送到订阅API）
	if config.Monitor.Enabled {
		fmt.Println("👂 启动频道消息监听器...")
		ext.Log().Info("👂 启动频道消息监听器...")
		activeServices++
		go func() {
			errChan <- processor.StartMessageListener(ctx)
		}()
	}

	// 2. 启动 Telegram Bot（监听用户对话，执行转发）
	if config.Bot.Enabled {
		fmt.Println("🤖 启动 Telegram Bot...")
		ext.Log().Info("🤖 启动 Telegram Bot...")
		activeServices++
		go func() {
			errChan <- processor.StartTelegramBot(ctx)
		}()
	}

	fmt.Println("========================================")
	if activeServices > 0 {
		fmt.Printf("✅ %d 个服务已启动\n", activeServices)
		fmt.Println("⏳ 运行中... (按 Ctrl+C 退出)")
	} else {
		fmt.Println("⚠️  所有功能已禁用，处于待机状态")
		fmt.Println("💡 请完成配置后重启服务")
		fmt.Println("⏳ 按 Ctrl+C 退出")
	}
	fmt.Println("========================================")

	// 启动心跳
	// go processor.StartHeartbeat(ctx)

	// 如果没有活动服务，只等待上下文取消
	if activeServices == 0 {
		<-ctx.Done()
		ext.Log().Info("收到停止信号，正在关闭...")
		return nil
	}

	// 等待任何协程出错或上下文取消
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		ext.Log().Info("收到停止信号，正在关闭...")
		return nil
	}
}
