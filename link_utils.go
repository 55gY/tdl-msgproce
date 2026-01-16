package main

import (
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

// buildLinkRegex 根据配置构建链接提取的正则表达式
// 如果配置中的 subs 和 ss 都为空，则使用默认协议列表
func buildLinkRegex(config *Config, log *zap.Logger) *regexp.Regexp {
	// 默认协议列表（与 config.yaml 保持一致，按长度降序排列）
	defaultProtocols := []string{
		"hysteria2", "wireguard", "hysteria", "juicity",
		"anytls", "mieru", "snell", "socks", "trojan", "vmess", "vless",
		"https", "tuic", "http", "ssr", "hy2", "ss", "sudoku",
	}

	// 从配置中提取协议（去除 :// 后缀）
	var protocols []string
	seen := make(map[string]bool) // 用于去重，保持顺序

	// 合并 subs 和 ss 协议列表
	allConfigProtocols := append(config.Monitor.Filters.Subs, config.Monitor.Filters.SS...)

	for _, p := range allConfigProtocols {
		// 去除 :// 后缀并转小写
		protocol := strings.ToLower(strings.TrimSuffix(p, "://"))
		if protocol != "" && !seen[protocol] {
			seen[protocol] = true
			protocols = append(protocols, protocol)
		}
	}

	// 如果配置为空，使用默认值
	if len(protocols) == 0 {
		protocols = defaultProtocols
		if log != nil {
			log.Debug("配置中未找到协议列表，使用默认协议",
				zap.Int("count", len(defaultProtocols)))
		}
	}

	// 构建正则表达式模式
	// (?i) - 不区分大小写
	// (?:...) - 非捕获组
	// [^\s...] - 排除空白和中文标点符号
	pattern := fmt.Sprintf(
		`(?i)(?:%s)://[^\s\x{FF0C}\x{3002}\x{FF1F}\x{FF01}\x{FF1B}\x{FF1A}\x{201C}\x{201D}\x{2018}\x{2019}]+`,
		strings.Join(protocols, "|"),
	)

	return regexp.MustCompile(pattern)
}

// ExtractAllLinks 从文本中提取所有链接（包括订阅链接和代理节点链接）
// 使用预编译的正则表达式，协议列表从配置文件动态读取
func (p *MessageProcessor) ExtractAllLinks(text string) []string {
	return p.linkRegex.FindAllString(text, -1)
}

// IsProxyNode 判断链接是否为代理节点链接（从配置文件读取协议列表）
func (p *MessageProcessor) IsProxyNode(link string) bool {
	linkLower := strings.ToLower(link)
	// 从配置文件读取节点协议列表
	for _, prefix := range p.config.Monitor.Filters.SS {
		prefixLower := strings.ToLower(prefix)
		if strings.HasPrefix(linkLower, prefixLower) {
			return true
		}
	}
	return false
}

// FilterLinks 过滤黑名单链接
func (p *MessageProcessor) FilterLinks(links []string, blacklist []string) []string {
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
