package main

import (
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// LineCountingWriter 按行数轮转的日志写入器
type LineCountingWriter struct {
	mu         sync.Mutex
	lumberjack *lumberjack.Logger
	lineCount  int
	maxLines   int // 例如 20000 行
}

// NewLineCountingWriter 创建按行数轮转的日志写入器
func NewLineCountingWriter(filename string, maxLines int) *LineCountingWriter {
	return &LineCountingWriter{
		lumberjack: &lumberjack.Logger{
			Filename:   filename,
			MaxBackups: 5,      // 保留 5 个备份文件
			Compress:   true,   // 压缩旧日志
		},
		maxLines: maxLines,
	}
}

// Write 实现 io.Writer 接口
func (w *LineCountingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 计算本次写入的行数（统计 \n 数量）
	lines := 0
	for _, b := range p {
		if b == '\n' {
			lines++
		}
	}

	// 写入前检查是否需要轮转
	if w.lineCount+lines > w.maxLines {
		if err := w.lumberjack.Rotate(); err != nil {
			return 0, err
		}
		w.lineCount = 0
	}

	// 执行实际写入
	n, err = w.lumberjack.Write(p)
	if err == nil {
		w.lineCount += lines
	}

	return n, err
}

// Sync 实现 zapcore.WriteSyncer 接口
func (w *LineCountingWriter) Sync() error {
	return w.lumberjack.Close()
}
