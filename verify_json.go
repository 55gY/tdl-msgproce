package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/core/util/tutil"
	"go.uber.org/zap"
)

// JSONMessage JSON文件中的消息格式
type JSONMessage struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}

// JSONExport Telegram导出的JSON格式
type JSONExport struct {
	ID       int64         `json:"id"`
	Messages []JSONMessage `json:"messages"`
}

// VerifyResult 验证结果
type VerifyResult struct {
	TotalMessages   int
	ValidMessages   int
	InvalidMessages int
	InvalidIDs      []int
	FirstErrorIndex int
	FirstErrorID    int
	ErrorMessage    string
}

// VerifyJSONMessages 验证JSON文件中的消息ID是否有效
func (p *MessageProcessor) VerifyJSONMessages(ctx context.Context, jsonFile string) (*VerifyResult, error) {
	p.ext.Log().Info("开始验证JSON文件", zap.String("file", jsonFile))

	// 读取JSON文件
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 解析JSON
	var export JSONExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	if export.ID == 0 {
		return nil, fmt.Errorf("无法从JSON中获取频道/群组ID")
	}

	// 过滤出类型为message的消息
	var messageIDs []int
	for _, msg := range export.Messages {
		if msg.Type == "message" && msg.ID > 0 {
			messageIDs = append(messageIDs, msg.ID)
		}
	}

	if len(messageIDs) == 0 {
		return nil, fmt.Errorf("JSON中没有有效的消息")
	}

	p.ext.Log().Info("开始验证消息", 
		zap.Int64("channelID", export.ID),
		zap.Int("totalMessages", len(messageIDs)))

	result := &VerifyResult{
		TotalMessages:   len(messageIDs),
		FirstErrorIndex: -1,
	}

	// 获取频道/群组信息
	client := p.ext.Client()
	api := client.API()
	kvd := newMemoryStorage()
	manager := peers.Options{Storage: storage.NewPeers(kvd)}.Build(api)

	peer, err := tutil.GetInputPeer(ctx, manager, fmt.Sprintf("%d", export.ID))
	if err != nil {
		return nil, fmt.Errorf("获取频道/群组信息失败: %w", err)
	}

	p.ext.Log().Info("频道/群组信息", 
		zap.Int64("id", peer.ID()),
		zap.String("name", peer.VisibleName()))

	// 使用20线程并发验证消息
	const numWorkers = 20
	jobs := make(chan struct{
		index int
		msgID int
	}, len(messageIDs))
	results := make(chan struct{
		index      int
		msgID      int
		isValid    bool
		errMessage string
	}, len(messageIDs))

	// 启动工作协程
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				// 尝试获取消息
				msg, err := tutil.GetSingleMessage(ctx, api, peer.InputPeer(), job.msgID)
				if err != nil {
					results <- struct {
						index      int
						msgID      int
						isValid    bool
						errMessage string
					}{job.index, job.msgID, false, err.Error()}
					continue
				}

				// 验证消息ID是否匹配
				if msg.GetID() != job.msgID {
					results <- struct {
						index      int
						msgID      int
						isValid    bool
						errMessage string
					}{job.index, job.msgID, false, fmt.Sprintf("消息ID不匹配，期望:%d 实际:%d", job.msgID, msg.GetID())}
					continue
				}

				results <- struct {
					index      int
					msgID      int
					isValid    bool
					errMessage string
				}{job.index, job.msgID, true, ""}

				// 添加短暂延迟避免触发限流
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	// 发送任务
	for i, msgID := range messageIDs {
		jobs <- struct {
			index int
			msgID int
		}{i, msgID}
	}
	close(jobs)

	// 等待所有工作完成
	go func() {
		wg.Wait()
		close(results)
	}()

	// 收集结果（带进度显示）
	processed := 0
	lastProgress := 0
	for res := range results {
		processed++
		
		// 显示进度（每10%）
		progress := processed * 100 / len(messageIDs)
		if progress >= lastProgress+10 {
			fmt.Printf("验证进度: %d%% (%d/%d)\n", progress, processed, len(messageIDs))
			lastProgress = progress
		}

		if !res.isValid {
			result.InvalidMessages++
			result.InvalidIDs = append(result.InvalidIDs, res.msgID)
			
			if result.FirstErrorIndex == -1 || res.index < result.FirstErrorIndex {
				result.FirstErrorIndex = res.index
				result.FirstErrorID = res.msgID
				result.ErrorMessage = res.errMessage
				
				p.ext.Log().Warn("发现无效消息", 
					zap.Int("index", res.index),
					zap.Int("messageID", res.msgID),
					zap.String("error", res.errMessage))
			}
		} else {
			result.ValidMessages++
		}
	}

	p.ext.Log().Info("验证完成", 
		zap.Int("total", result.TotalMessages),
		zap.Int("valid", result.ValidMessages),
		zap.Int("invalid", result.InvalidMessages))

	return result, nil
}

// CreateCleanedJSON 创建清理后的JSON文件（移除无效消息）
func (p *MessageProcessor) CreateCleanedJSON(originalFile string, outputFile string, invalidIDs []int) error {
	p.ext.Log().Info("创建清理后的JSON", 
		zap.String("input", originalFile),
		zap.String("output", outputFile),
		zap.Int("removeCount", len(invalidIDs)))

	// 读取原始JSON
	data, err := os.ReadFile(originalFile)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	var export JSONExport
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("解析JSON失败: %w", err)
	}

	// 创建无效ID的map用于快速查找
	invalidMap := make(map[int]bool)
	for _, id := range invalidIDs {
		invalidMap[id] = true
	}

	// 过滤消息
	var cleanedMessages []JSONMessage
	for _, msg := range export.Messages {
		if !invalidMap[msg.ID] {
			cleanedMessages = append(cleanedMessages, msg)
		}
	}

	export.Messages = cleanedMessages

	// 写入新文件
	output, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化JSON失败: %w", err)
	}

	if err := os.WriteFile(outputFile, output, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	p.ext.Log().Info("清理后的JSON已保存", 
		zap.String("file", outputFile),
		zap.Int("originalCount", len(export.Messages)+len(invalidIDs)),
		zap.Int("cleanedCount", len(cleanedMessages)))

	return nil
}

// VerifyAndFixJSON 验证JSON并创建修复版本
func (p *MessageProcessor) VerifyAndFixJSON(ctx context.Context, jsonFile string) error {
	fmt.Println("🔍 开始验证JSON文件...")
	
	result, err := p.VerifyJSONMessages(ctx, jsonFile)
	if err != nil {
		return fmt.Errorf("验证失败: %w", err)
	}

	// 打印结果
	fmt.Println("\n📊 验证结果:")
	fmt.Printf("总消息数: %d\n", result.TotalMessages)
	fmt.Printf("✅ 有效消息: %d (%.1f%%)\n", 
		result.ValidMessages, 
		float64(result.ValidMessages)*100/float64(result.TotalMessages))
	fmt.Printf("❌ 无效消息: %d (%.1f%%)\n", 
		result.InvalidMessages,
		float64(result.InvalidMessages)*100/float64(result.TotalMessages))

	if result.FirstErrorIndex >= 0 {
		fmt.Printf("\n⚠️  第一个错误位置:\n")
		fmt.Printf("   索引: %d (第%d条消息)\n", result.FirstErrorIndex, result.FirstErrorIndex+1)
		fmt.Printf("   消息ID: %d\n", result.FirstErrorID)
		fmt.Printf("   错误: %s\n", result.ErrorMessage)
	}

	// 如果有无效消息，创建清理版本
	if result.InvalidMessages > 0 {
		outputFile := jsonFile[:len(jsonFile)-5] + "_cleaned.json"
		fmt.Printf("\n🔧 正在创建清理后的JSON: %s\n", outputFile)
		
		if err := p.CreateCleanedJSON(jsonFile, outputFile, result.InvalidIDs); err != nil {
			return fmt.Errorf("创建清理版本失败: %w", err)
		}

		fmt.Printf("✅ 清理完成！新文件已保存\n")
		fmt.Printf("   原始文件: %s (%d条消息)\n", jsonFile, result.TotalMessages)
		fmt.Printf("   清理文件: %s (%d条消息)\n", outputFile, result.ValidMessages)
		fmt.Println("\n💡 建议使用清理后的文件进行转发")
	} else {
		fmt.Println("\n✅ 所有消息都有效，无需清理！")
	}

	return nil
}
