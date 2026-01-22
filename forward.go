package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/iyear/tdl/app/forward"
	"github.com/iyear/tdl/core/forwarder"
	"github.com/iyear/tdl/core/storage"
	"go.uber.org/zap"
)

// memoryStorage 简单的内存 storage 实现
type memoryStorage struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMemoryStorage() storage.Storage {
	return &memoryStorage{
		data: make(map[string][]byte),
	}
}

func (m *memoryStorage) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if val, ok := m.data[key]; ok {
		return val, nil
	}
	return nil, storage.ErrNotFound
}

func (m *memoryStorage) Set(ctx context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = value
	return nil
}

func (m *memoryStorage) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	return nil
}

// cleanANSI 清理ANSI转义字符
func cleanANSI(s string) string {
	// 移除ANSI颜色代码和控制字符
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiRegex.ReplaceAllString(s, "")
}

// progressWriter 捕获tdl输出并解析进度
type progressWriter struct {
	original       io.Writer
	progressRegexp *regexp.Regexp
	onProgress     func(percent int, line string)
}

func newProgressWriter(original io.Writer, onProgress func(percent int, line string)) *progressWriter {
	// 匹配多种进度格式:
	// ... 58.5% [...]
	// 58.5%
	// [58.5%]
	return &progressWriter{
		original:       original,
		progressRegexp: regexp.MustCompile(`(\d+\.\d+)%`),
		onProgress:     onProgress,
	}
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	// 写入原始输出
	n, err = pw.original.Write(p)

	// 解析进度
	if pw.onProgress != nil {
		line := string(p)
		matches := pw.progressRegexp.FindAllStringSubmatch(line, -1)
		if len(matches) > 0 {
			// 找到最后一个匹配的百分比
			lastMatch := matches[len(matches)-1]
			if len(lastMatch) > 1 {
				if percent, parseErr := strconv.ParseFloat(lastMatch[1], 64); parseErr == nil {
					// 清理ANSI转义字符和换行符
					cleanLine := cleanANSI(line)
					cleanLine = strings.TrimSpace(strings.ReplaceAll(cleanLine, "\n", " "))

					// 只保留关键信息：去掉CPU/Memory/Goroutines部分
					if idx := strings.Index(cleanLine, "("); idx > 0 {
						cleanLine = cleanLine[idx:]
					}

					// 限制长度到150字符
					if len(cleanLine) > 150 {
						cleanLine = cleanLine[:150] + "..."
					}

					// 四舍五入进度值
					roundedPercent := int(percent + 0.5)
					pw.onProgress(roundedPercent, cleanLine)
				}
			}
		}
	}

	return n, err
}

// forwardFromLink 使用 tdl 的 forward 功能转发消息
// 支持格式:
// - https://t.me/channel/123
// - https://t.me/c/1234567890/123
// - @channel_username
// target: 可选的转发目标 ID，为 nil 时使用配置文件中的默认目标
func (p *MessageProcessor) forwardFromLink(ctx context.Context, link string, target *int64, onProgress func(int, string)) error {
	p.ext.Log().Info("开始转发", zap.String("link", link))

	// 确定转发目标
	var targetID int64
	if target != nil {
		targetID = *target
	} else {
		targetID = p.config.Bot.ForwardTarget
	}

	if onProgress == nil {
		// 如果没有进度回调，直接执行
		kvd := newMemoryStorage()
		var mode forwarder.Mode
		switch p.config.Bot.ForwardMode {
		case "clone":
			mode = forwarder.ModeClone
		case "direct":
			mode = forwarder.ModeDirect
		default:
			mode = forwarder.ModeClone
		}
		
		opts := forward.Options{
			From:   []string{link},            // 转发源：频道链接
			To:     fmt.Sprintf("%d", targetID), // 转发目标：目标频道或群组 ID
			Mode:   mode,                      // 转发模式：clone(克隆) 或 direct(直接转发)
			Silent: false,                     // 是否静默转发：true 时不通知接收者
			DryRun: false,                     // 是否空运行：true 时仅模拟不实际执行
			Single: true,                      // 是否单条模式：true 时逐条转发
			Desc:   false,                     // 是否降序
		}
		client := p.ext.Client()
		if err := forward.Run(ctx, client, kvd, opts); err != nil {
			return fmt.Errorf("转发失败: %w", err)
		}
		p.ext.Log().Info("转发成功", zap.String("link", link))
		return nil
	}

	// 保存原始 stdout 和 stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	// 创建管道捕获输出
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	// 启动goroutine读取输出并解析进度
	done := make(chan bool, 2)

	// 读取 stdout
	go func() {
		pw := newProgressWriter(oldStdout, onProgress)
		io.Copy(pw, rOut)
		done <- true
	}()

	// 读取 stderr
	go func() {
		pw := newProgressWriter(oldStderr, onProgress)
		io.Copy(pw, rErr)
		done <- true
	}()

	// 使用内存存储
	kvd := newMemoryStorage()

	// 转换 forward mode
	var mode forwarder.Mode
	switch p.config.Bot.ForwardMode {
	case "clone":
		mode = forwarder.ModeClone
	case "direct":
		mode = forwarder.ModeDirect
	default:
		mode = forwarder.ModeClone
	}

	// 准备 forward 选项
	opts := forward.Options{
		From:   []string{link},            // 转发源：频道链接
		To:     fmt.Sprintf("%d", targetID), // 转发目标：目标频道或群组 ID
		Mode:   mode,                      // 转发模式：clone(克隆) 或 direct(直接转发)
		Silent: false,                     // 是否静默转发：true 时不通知接收者
		DryRun: false,                     // 是否空运行：true 时仅模拟不实际执行
		Single: true,                      // 是否单条模式：true 时逐条转发
		Desc:   false,                     // 是否降序
	}

	// 调用 tdl 的 forward 功能
	client := p.ext.Client()
	err := forward.Run(ctx, client, kvd, opts)

	// 关闭写入端，等待读取完成
	wOut.Close()
	wErr.Close()
	<-done
	<-done

	if err != nil {
		return fmt.Errorf("转发失败: %w", err)
	}

	p.ext.Log().Info("转发成功", zap.String("link", link), zap.Int64("target", targetID))
	return nil
}

// parseJSONMessages 解析导出的 JSON 文件并返回消息链接数组
func (p *MessageProcessor) parseJSONMessages(jsonFilePath string) ([]string, error) {
	// 定义 JSON 结构
	type ExportData struct {
		ID       int64 `json:"id"`
		Messages []struct {
			ID int `json:"id"`
		} `json:"messages"`
	}

	// 读取文件
	data, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 解析 JSON
	var export ExportData
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	// 转换频道 ID（处理负数情况）
	channelID := export.ID
	if channelID < 0 {
		channelID = -channelID - 1000000000000
	}

	// 构造消息链接
	var links []string
	for _, msg := range export.Messages {
		// 构造 t.me 链接
		link := fmt.Sprintf("https://t.me/c/%d/%d", channelID, msg.ID)
		links = append(links, link)
	}

	if len(links) == 0 {
		return nil, fmt.Errorf("未找到任何可转发的消息")
	}

	p.ext.Log().Info("解析 JSON 完成", 
		zap.String("file", jsonFilePath), 
		zap.Int64("channelID", channelID),
		zap.Int("messageCount", len(links)))

	return links, nil
}


