package main

import (
	"container/list"
	"sync"
)

// MessageCache LRU 缓存，用于消息去重
type MessageCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[int]*list.Element
	lru      *list.List // 双向链表，记录顺序
}

// cacheEntry 缓存条目
type cacheEntry struct {
	messageID int
}

// NewMessageCache 创建一个新的消息缓存
func NewMessageCache(capacity int) *MessageCache {
	return &MessageCache{
		capacity: capacity,
		cache:    make(map[int]*list.Element),
		lru:      list.New(),
	}
}

// Has 检查消息是否已在缓存中
func (mc *MessageCache) Has(messageID int) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	_, exists := mc.cache[messageID]
	return exists
}

// Add 添加消息到缓存，如果容量已满则删除最旧的
func (mc *MessageCache) Add(messageID int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// 如果已存在，移到前面（最近使用）
	if elem, exists := mc.cache[messageID]; exists {
		mc.lru.MoveToFront(elem)
		return
	}

	// 检查容量，超出则删除最旧的
	if mc.lru.Len() >= mc.capacity {
		oldest := mc.lru.Back()
		if oldest != nil {
			mc.lru.Remove(oldest)
			delete(mc.cache, oldest.Value.(*cacheEntry).messageID)
		}
	}

	// 添加新元素
	entry := &cacheEntry{messageID: messageID}
	elem := mc.lru.PushFront(entry)
	mc.cache[messageID] = elem
}

// Len 返回当前缓存中的消息数量
func (mc *MessageCache) Len() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.lru.Len()
}
