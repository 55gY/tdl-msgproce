package main

import (
	"container/list"
	"fmt"
	"sync"
)

// MessageCache LRU 缓存，用于消息去重
type MessageCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element // 键格式: "channelID_messageID"
	lru      *list.List               // 双向链表，记录顺序
}

// cacheEntry 缓存条目
type cacheEntry struct {
	channelID int64 // 频道/对话 ID
	messageID int   // 消息 ID
	editDate  int   // 编辑时间戳，0 表示未编辑
}

// NewMessageCache 创建一个新的消息缓存
func NewMessageCache(capacity int) *MessageCache {
	return &MessageCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

// GetCacheKey 生成缓存键
func GetCacheKey(channelID int64, messageID int) string {
	return fmt.Sprintf("%d_%d", channelID, messageID)
}

// Has 检查消息是否已在缓存中
func (mc *MessageCache) Has(channelID int64, messageID int) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	key := GetCacheKey(channelID, messageID)
	_, exists := mc.cache[key]
	return exists
}

// Add 添加消息到缓存，如果容量已满则删除最旧的
func (mc *MessageCache) Add(channelID int64, messageID int, editDate int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := GetCacheKey(channelID, messageID)

	// 如果已存在，移到前面（最近使用）
	if elem, exists := mc.cache[key]; exists {
		mc.lru.MoveToFront(elem)
		// 更新编辑时间
		elem.Value.(*cacheEntry).editDate = editDate
		return
	}

	// 检查容量，超出则删除最旧的
	if mc.lru.Len() >= mc.capacity {
		oldest := mc.lru.Back()
		if oldest != nil {
			mc.lru.Remove(oldest)
			oldEntry := oldest.Value.(*cacheEntry)
			oldKey := GetCacheKey(oldEntry.channelID, oldEntry.messageID)
			delete(mc.cache, oldKey)
		}
	}

	// 添加新元素
	entry := &cacheEntry{
		channelID: channelID,
		messageID: messageID,
		editDate:  editDate,
	}
	elem := mc.lru.PushFront(entry)
	mc.cache[key] = elem
}

// AddOrUpdate 添加或更新消息，返回是否为编辑更新（内容有变化）
// 返回值: (isEdit bool, shouldProcess bool)
// - isEdit: 消息是否已存在（即是否为编辑）
// - shouldProcess: 是否应该处理此消息（新消息或编辑时间更新）
func (mc *MessageCache) AddOrUpdate(channelID int64, messageID int, editDate int) (isEdit bool, shouldProcess bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := GetCacheKey(channelID, messageID)

	// 检查是否已存在
	if elem, exists := mc.cache[key]; exists {
		oldEntry := elem.Value.(*cacheEntry)
		// 消息已存在，这是一次编辑
		if editDate > oldEntry.editDate {
			// 编辑时间更新，需要重新处理
			oldEntry.editDate = editDate
			mc.lru.MoveToFront(elem)
			return true, true
		}
		// 编辑时间未变，可能是重复事件
		mc.lru.MoveToFront(elem)
		return true, false
	}

	// 新消息，添加到缓存
	if mc.lru.Len() >= mc.capacity {
		oldest := mc.lru.Back()
		if oldest != nil {
			mc.lru.Remove(oldest)
			oldEntry := oldest.Value.(*cacheEntry)
			oldKey := GetCacheKey(oldEntry.channelID, oldEntry.messageID)
			delete(mc.cache, oldKey)
		}
	}

	entry := &cacheEntry{
		channelID: channelID,
		messageID: messageID,
		editDate:  editDate,
	}
	elem := mc.lru.PushFront(entry)
	mc.cache[key] = elem

	// 新消息，需要处理
	return false, true
}

// Len 返回当前缓存中的消息数量
func (mc *MessageCache) Len() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.lru.Len()
}
