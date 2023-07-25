package geecache

import (
	"geecache/lru"
	"sync"
)

/*
	cache 结构体：实例化 lru，封装 get, add, remove 等方法，
	并添加互斥锁 mu，实现的并发缓存
*/

type cache struct {
	mu     sync.RWMutex
	lru    *lru.LRUCache
	nbytes int64 // size of all keys and values, 即当前已使用的内存
}

func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 判断 cache 中的 lru 是否为 nil，若是则新建 lru 实例，这种方法称之为延迟初始化，
	// 延迟初始化意味着该对象的创建将会延迟至第一次使用该对象时，主要用于提高性能，并减少程序内存要求。
	if c.lru == nil {
		c.lru = lru.New(0, func(key string, value interface{}) {
			val := value.(ByteView)
			c.nbytes -= int64(len(key)) + val.Len()
		})
	}
	c.lru.Add(key, value, value.e)
	c.nbytes += int64(len(key)) + value.Len()
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

func (c *cache) removeOldest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.RemoveOldest()
	}
}

func (c *cache) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.Remove(key)
	}
}

// 返回cache中key+value的累计大小
func (c *cache) bytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nbytes
}
