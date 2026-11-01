// tdl-msgproce - 配置备份功能
//
// /backup 命令：完整备份 tdl 与 tdl-msgproce 的配置文件（含登录会话文件），
// 不含主程序二进制，按目录结构打包为 tar.gz，通过 Bot 发送给请求用户。
//
// 日志输出规范：
// - 使用 fmt.Printf() 输出用户可见的日志信息
// - 调试日志使用 // fmt.Printf() 注释格式
// - 不使用 zap 日志库
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// 备份纳入清单（相对于 tdl 根目录，如 ~/.tdl）
var backupIncludeSpecs = []string{
	"config.toml",              // tdl 主配置
	"data.kv",                  // tdl 主 kv 数据库文件
	"session",                  // tdl 登录会话（含登录配置文件）
	"data",                     // tdl 内部数据目录
	"extensions/data/msgproce", // tdl-msgproce 扩展数据目录（含 config.yaml）
}

// 备份排除目录（相对于上面的纳入项，目录名前缀匹配）
var backupExcludeDirs = []string{
	"extensions/data/msgproce/log", // 运行日志，不属于配置
}

// backupEntry 备份文件条目
type backupEntry struct {
	absPath string // 磁盘绝对路径
	arcPath string // 压缩包内路径（保留目录结构，如 .tdl/config.toml）
}

// tdlRootDir 从扩展 DataDir 反推 tdl 根目录
// DataDir = <root>/extensions/data/msgproce，因此向上 3 级即为 tdl 根目录
func (p *MessageProcessor) tdlRootDir() (string, error) {
	dataDir := p.ext.Config().DataDir
	root := dataDir
	for i := 0; i < 3; i++ {
		root = filepath.Dir(root)
	}
	// 简单校验：DataDir 必须确实位于 root/extensions/data/msgproce 之下
	expect := filepath.Join(root, "extensions", "data", "msgproce")
	if filepath.Clean(dataDir) != filepath.Clean(expect) {
		return "", fmt.Errorf("DataDir 结构异常，无法定位 tdl 根目录 (dataDir=%s)", dataDir)
	}
	return root, nil
}

// collectBackupEntries 收集备份文件清单
func collectBackupEntries(root string) ([]backupEntry, error) {
	var entries []backupEntry
	rootBase := filepath.Base(root) // 通常为 .tdl，压缩包内以它为根保留目录结构

	isExcluded := func(rel string) bool {
		rel = filepath.ToSlash(rel)
		for _, ex := range backupExcludeDirs {
			if rel == ex || strings.HasPrefix(rel, ex+"/") {
				return true
			}
		}
		return false
	}

	for _, spec := range backupIncludeSpecs {
		absSpec := filepath.Join(root, spec)
		info, err := os.Stat(absSpec)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("⚠️  备份跳过不存在的路径: %s\n", spec)
				continue
			}
			return nil, fmt.Errorf("读取备份路径失败 %s: %w", spec, err)
		}

		walkFn := func(path string, d os.FileInfo, err error) error {
			if err != nil {
				fmt.Printf("⚠️  备份跳过不可读路径 %s: %v\n", path, err)
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if isExcluded(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			// 只收录普通文件，跳过符号链接/设备等特殊文件
			if !d.Mode().IsRegular() {
				return nil
			}
			entries = append(entries, backupEntry{
				absPath: path,
				arcPath: filepath.ToSlash(filepath.Join(rootBase, rel)),
			})
			return nil
		}

		if info.IsDir() {
			if err := filepath.Walk(absSpec, walkFn); err != nil {
				return nil, fmt.Errorf("遍历备份目录失败 %s: %w", spec, err)
			}
		} else {
			if err := walkFn(absSpec, info, nil); err != nil {
				return nil, err
			}
		}
	}

	// 排序，保证压缩包内容稳定可预期
	sort.Slice(entries, func(i, j int) bool { return entries[i].arcPath < entries[j].arcPath })
	return entries, nil
}

// createBackupArchive 将备份清单打包为 tar.gz
func createBackupArchive(entries []backupEntry, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, e := range entries {
		info, err := os.Stat(e.absPath)
		if err != nil {
			fmt.Printf("⚠️  打包跳过 %s: %v\n", e.arcPath, err)
			continue
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("生成 tar 头失败 %s: %w", e.arcPath, err)
		}
		hdr.Name = e.arcPath
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("写入 tar 头失败 %s: %w", e.arcPath, err)
		}
		f, err := os.Open(e.absPath)
		if err != nil {
			return fmt.Errorf("打开文件失败 %s: %w", e.arcPath, err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return fmt.Errorf("写入文件内容失败 %s: %w", e.arcPath, err)
		}
		f.Close()
	}
	return nil
}

// handleBackupCommand 处理 /backup 命令：打包配置并通过 Bot 发送给请求用户
func (p *MessageProcessor) handleBackupCommand(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, "📦 正在打包 tdl 配置文件，请稍候...")

	root, err := p.tdlRootDir()
	if err != nil {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, fmt.Sprintf("❌ 备份失败：%v", err))
		return
	}

	entries, err := collectBackupEntries(root)
	if err != nil {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, fmt.Sprintf("❌ 收集备份文件失败：%v", err))
		return
	}
	if len(entries) == 0 {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, "❌ 没有找到可备份的配置文件")
		return
	}

	stamp := time.Now().Format("20060102-150405")
	archiveName := fmt.Sprintf("tdl-config-backup-%s.tar.gz", stamp)
	archivePath := filepath.Join(os.TempDir(), archiveName)
	// 发送完成后删除临时包
	defer os.Remove(archivePath)

	if err := createBackupArchive(entries, archivePath); err != nil {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, fmt.Sprintf("❌ 打包失败：%v", err))
		return
	}

	stat, err := os.Stat(archivePath)
	if err != nil {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, fmt.Sprintf("❌ 读取备份包失败：%v", err))
		return
	}
	fmt.Printf("✅ 配置备份打包完成: %s (%d 个文件, %d 字节)\n", archivePath, len(entries), stat.Size())

	caption := fmt.Sprintf("📦 tdl 配置备份\n🕐 %s\n📁 %d 个文件，共 %s\n\n⚠️ 包含登录会话与 Bot Token，请妥善保管",
		time.Now().Format("2006-01-02 15:04:05"), len(entries), formatBytes(stat.Size()))
	doc := tgbotapi.NewDocument(msg.Chat.ID, tgbotapi.FilePath(archivePath))
	doc.Caption = caption
	if _, err := bot.Send(doc); err != nil {
		p.sendBotReply(bot, msg.Chat.ID, msg.MessageID, fmt.Sprintf("❌ 发送备份包失败：%v", err))
		return
	}
	fmt.Printf("✅ 配置备份已发送给用户 %d\n", msg.From.ID)
}

// formatBytes 字节数转人类可读
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
