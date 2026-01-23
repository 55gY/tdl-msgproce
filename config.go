package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 总配置结构
type Config struct {
	Bot     BotConfig     `yaml:"bot"`
	Monitor MonitorConfig `yaml:"monitor"`
	Proxy   ProxyConfig   `yaml:"proxy"`
}

// BotConfig Telegram Bot 配置
type BotConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Token         string  `yaml:"token"`
	AllowedUsers  []int64 `yaml:"allowed_users"`
	ForwardTarget int64   `yaml:"forward_target"`
	ForwardMode   string  `yaml:"forward_mode"` // clone 或 copy
}

// MonitorConfig 消息监听配置
type MonitorConfig struct {
	Enabled bool `yaml:"enabled"`

	SubscriptionAPI struct {
		ApiKey string `yaml:"api_key"`
		AddURL string `yaml:"add_url"` // 添加订阅的完整 URL
	} `yaml:"subscription_api"`

	Features struct {
		FetchHistoryCount int `yaml:"fetch_history_count"` // 获取历史消息数量（>0开启，<=0关闭）
	} `yaml:"features"`

	Channels          []int64 `yaml:"channels"`
	WhitelistChannels []int64 `yaml:"whitelist_channels"`

	Filters struct {
		Subs          []string `yaml:"subs"`           // 订阅格式过滤（需要二次过滤）
		SS            []string `yaml:"ss"`             // 节点格式过滤（不需要二次过滤）
		ContentFilter []string `yaml:"content_filter"` // 二次内容过滤（仅对订阅生效）
		LinkBlacklist []string `yaml:"link_blacklist"`
	} `yaml:"filters"`
}

// ProxyConfig HTTP 代理配置（用于订阅解析）
type ProxyConfig struct {
	Enabled       bool   `yaml:"enabled"`        // 是否启用代理服务
	Host          string `yaml:"host"`           // 监听地址（0.0.0.0 允许外部访问，127.0.0.1 仅本地）
	Port          int    `yaml:"port"`           // 监听端口
	Token         string `yaml:"token"`          // 访问 Token
	Timeout       int    `yaml:"timeout"`        // 请求超时时间（秒）
	MaxConcurrent int    `yaml:"max_concurrent"` // 最大并发请求数
}

// loadConfig 加载配置文件
func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 自动检测并禁用未配置的功能
	validateAndDisableFeatures(&config)

	return &config, nil
}

// validateAndDisableFeatures 验证并自动禁用未配置的功能
func validateAndDisableFeatures(config *Config) {
	// 检查 Bot 配置
	if config.Bot.Token == "" ||
		config.Bot.Token == "YOUR_BOT_TOKEN" ||
		len(config.Bot.Token) < 20 {
		if config.Bot.Enabled {
			fmt.Println("⚠️  Bot Token 未配置或无效，自动禁用 Bot 功能")
		}
		config.Bot.Enabled = false
	}

	// 检查 Monitor 配置
	monitorValid := true

	// 检查订阅 API 配置
	if config.Monitor.SubscriptionAPI.ApiKey == "" ||
		config.Monitor.SubscriptionAPI.ApiKey == "YOUR_API_KEY" ||
		config.Monitor.SubscriptionAPI.AddURL == "" ||
		config.Monitor.SubscriptionAPI.AddURL == "YOUR_API_ADD_URL" {
		monitorValid = false
		if config.Monitor.Enabled {
			fmt.Println("⚠️  订阅 API 配置未完成，自动禁用 Monitor 功能")
		}
	} // 检查是否有监听频道
	if len(config.Monitor.Channels) == 0 {
		monitorValid = false
		if config.Monitor.Enabled {
			fmt.Println("⚠️  未配置监听频道，自动禁用 Monitor 功能")
		}
	}

	if !monitorValid {
		config.Monitor.Enabled = false
	}

	// 如果两个功能都被禁用，给出警告
	if !config.Bot.Enabled && !config.Monitor.Enabled {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("⚠️  警告：所有功能已禁用")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("")
		fmt.Println("请检查配置文件并完成以下配置：")
		fmt.Println("")
		fmt.Println("启用 Bot 功能需要配置：")
		fmt.Println("  bot.token - 从 @BotFather 获取的 Bot Token")
		fmt.Println("")
		fmt.Println("启用 Monitor 功能需要配置：")
		fmt.Println("  subscription_api.api_key - API 密钥")
		fmt.Println("  subscription_api.add_url - 添加订阅的完整 URL")
		fmt.Println("  monitor.channels - 要监听的频道ID列表")
		fmt.Println("")
		fmt.Println("配置文件路径: 请查看启动日志")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}
}
