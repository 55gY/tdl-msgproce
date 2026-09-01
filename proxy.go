// tdl-msgproce - HTTP 代理服务器（用于订阅解析）
//
// 日志输出规范：
// - 使用 fmt.Printf() 输出用户可见的日志信息
// - 调试日志使用 // fmt.Printf() 注释格式
// - 不使用 zap 日志库
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProxyServer HTTP 代理服务器（用于订阅解析）
type ProxyServer struct {
	cfg       *ProxyConfig
	server    *http.Server
	semaphore chan struct{} // 并发限制
}

// NewProxyServer 创建新的代理服务器实例
func NewProxyServer(cfg *ProxyConfig) *ProxyServer {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	ps := &ProxyServer{
		cfg:       cfg,
		semaphore: make(chan struct{}, maxConcurrent),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sub", ps.handleProxy)

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ps.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      timeout,
		IdleTimeout:       60 * time.Second,
	}

	return ps
}

// handleProxy 处理代理请求
func (ps *ProxyServer) handleProxy(w http.ResponseWriter, r *http.Request) {
	// 并发控制：获取令牌
	select {
	case ps.semaphore <- struct{}{}:
		defer func() { <-ps.semaphore }()
	default:
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	// 验证 Token
	token := r.URL.Query().Get("token")
	if token != ps.cfg.Token {
		http.Error(w, "Forbidden: Invalid Token", http.StatusForbidden)
		return
	}

	// 获取目标 URL
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "Missing 'url' parameter", http.StatusBadRequest)
		return
	}

	if err := validateProxyTarget(r.Context(), targetURL); err != nil {
		http.Error(w, "Forbidden target URL", http.StatusForbidden)
		return
	}

	// 创建中转请求
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Invalid target URL", http.StatusBadRequest)
		return
	}

	// 不透传客户端请求头，避免上游根据浏览器头返回差异内容
	proxyReq.Header.Set("User-Agent", "clash-verge/v2.4.7")

	// 创建 HTTP 客户端，设置超时
	timeout := time.Duration(ps.cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	// 执行请求
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Target unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 固定响应类型，不透传上游响应头
	w.Header().Set("Content-Type", "text/plain")

	// 透传上游响应状态码，仅透传响应内容
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		fmt.Printf("⚠️ 代理响应传输失败: %v\n", err)
	}
}

func validateProxyTarget(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("无效目标地址")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("仅允许 HTTP/HTTPS")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("禁止本地域名")
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("无法解析目标地址: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("禁止访问内网地址")
		}
	}
	return nil
}

// Start 启动代理服务器
func (ps *ProxyServer) Start(ctx context.Context) error {
	errChan := make(chan error, 1)

	go func() {
		errChan <- ps.server.ListenAndServe()
	}()

	select {
	case err := <-errChan:
		if err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ps.server.Shutdown(shutdownCtx)
	}
}

// Shutdown 优雅关闭代理服务器
func (ps *ProxyServer) Shutdown(ctx context.Context) error {
	return ps.server.Shutdown(ctx)
}
