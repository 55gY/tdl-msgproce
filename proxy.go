package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	ps := &ProxyServer{
		cfg:       cfg,
		semaphore: make(chan struct{}, cfg.MaxConcurrent),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sub", ps.handleProxy)

	ps.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler: mux,
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

	// 创建中转请求
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Invalid target URL", http.StatusBadRequest)
		return
	}

	// 复制请求头
	for name, values := range r.Header {
		proxyReq.Header[name] = values
	}

	// 创建 HTTP 客户端，设置超时
	client := &http.Client{
		Timeout: time.Duration(ps.cfg.Timeout) * time.Second,
	}

	// 执行请求
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Target unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	for name, values := range resp.Header {
		w.Header()[name] = values
	}

	// 写入状态码和响应体
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
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