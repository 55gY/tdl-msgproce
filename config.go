// tdl-msgproce - 配置文件加载和管理
//
// 日志输出规范：
// - 使用 fmt.Printf() 输出用户可见的日志信息
// - 调试日志使用 // fmt.Printf() 注释格式
// - 不使用 zap 日志库
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
	Twitter TwitterConfig `yaml:"twitter"`
}

// BotConfig Telegram Bot 配置
type BotConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Token         string  `yaml:"token"`
	AllowedUsers  []int64 `yaml:"allowed_users"`
	ForwardTarget int64   `yaml:"forward_target"`
	ForwardMode   string  `yaml:"forward_mode"` // clone 或 copy
}

// TwitterConfig X/Twitter 登录会话配置。
// 敏感值优先从 X_AUTH_TOKEN/X_CSRF_TOKEN 环境变量读取。
type TwitterConfig struct {
	AuthToken string `yaml:"auth_token"`
	CSRFToken string `yaml:"csrf_token"`
}

// MonitorConfig 消息监听配置
type MonitorConfig struct {
	Enabled bool `yaml:"enabled"`

	SubscriptionAPI struct {
		ApiKey string `yaml:"api_key"`
		AddURL string `yaml:"add_url"` // 添加订阅的完整 URL
	} `yaml:"subscription_api"`

	Features struct {
		FetchHistoryCount   int  `yaml:"fetch_history_count"`   // 获取历史消息数量（>0开启，<=0关闭）
		AutoRecloneForwards bool `yaml:"auto_reclone_forwards"` // 是否自动克隆 forward_target 频道的转发消息
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
	validateAndDisableFeatures(&config)
	return &config, nil
}

// validateAndDisableFeatures 验证并自动禁用未配置的功能
func validateAndDisableFeatures(config *Config) {
	if config.Bot.Token == "" || config.Bot.Token == "YOUR_BOT_TOKEN" || len(config.Bot.Token) < 20 {
		if config.Bot.Enabled {
			fmt.Println("⚠️  Bot Token 未配置或无效，自动禁用 Bot 功能")
		}
		config.Bot.Enabled = false
	}

	monitorValid := true
	if config.Monitor.SubscriptionAPI.ApiKey == "" ||
		config.Monitor.SubscriptionAPI.ApiKey == "YOUR_API_KEY" ||
		config.Monitor.SubscriptionAPI.AddURL == "" ||
		config.Monitor.SubscriptionAPI.AddURL == "YOUR_API_ADD_URL" {
		monitorValid = false
		if config.Monitor.Enabled {
			fmt.Println("⚠️  订阅 API 配置未完成，自动禁用 Monitor 功能")
		}
	}
	if len(config.Monitor.Channels) == 0 {
		monitorValid = false
		if config.Monitor.Enabled {
			fmt.Println("⚠️  未配置监听频道，自动禁用 Monitor 功能")
		}
	}
	if !monitorValid {
		config.Monitor.Enabled = false
	}

	if !config.Bot.Enabled && !config.Monitor.Enabled {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("⚠️  警告：所有功能已禁用")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("请检查配置文件并完成 Bot 或 Monitor 配置")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}
}
