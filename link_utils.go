package main

import (
	"regexp"
	"strings"
)

// ExtractAllLinks 从文本中提取所有链接（包括订阅链接和代理节点链接）
func (p *MessageProcessor) ExtractAllLinks(text string) []string {
	// 匹配 http/https 和各种代理协议链接，但排除中文标点符号
	re := regexp.MustCompile(`(?:https?|vmess|vless|ss|ssr|trojan|hysteria2?|hy2|tuic|juicity)://[^\s\x{FF0C}\x{3002}\x{FF1F}\x{FF01}\x{FF1B}\x{FF1A}\x{201C}\x{201D}\x{2018}\x{2019}]+`)
	return re.FindAllString(text, -1)
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
