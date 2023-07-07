package geecache

import (
	"project_cache/geecache/lru"
	"sync"
)

/*
	cache 结构体：实例化 lru，封装 get 和 add 方法，
	并添加互斥锁 mu，实现的并发缓存
*/

type cache struct {
	mu         sync.Mutex
	lru        *lru.LRUCache
	cacheBytes int64
}

// 判断 cache 中的 lru 是否为 nil，如果等于 nil 则新建 lru 实例，这种方法称之为延迟初始化
// 一个对象的延迟初始化意味着该对象的创建将会延迟至第一次使用该对象时，主要用于提高性能，并减少程序内存要求。
func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = lru.New(c.cacheBytes, nil)
	}
	c.lru.Add(key, value)
}

// 获取 key 在 lru 中对应的 Value，并断言为 ByteView 返回
func (c *cache) get(key string) (ByteView, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		return ByteView{}, false
	}
	if v, exist := c.lru.Get(key); exist {
		return v.(ByteView), exist
	}
	return ByteView{}, false
}
