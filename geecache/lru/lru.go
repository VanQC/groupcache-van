package lru

import (
	"container/list"
)

type LRUCache struct {
	maxBytes int64                    // 允许使用的最大内存
	nBytes   int64                    // 当前已使用的内存
	ll       *list.List               // 直接使用 Go 语言标准库实现的双向链表list.List
	keyLink    map[string]*list.Element // 键是字符串，值是双向链表中对应节点的指针

	// 某条记录被移除时的回调函数，可以为 nil
	OnEvicted func(key string, value Value)
}

type entry struct {
	key   string
	value Value
}

type Value interface {
	Len() int64 // 用于返回值所占用的内存大小
}

// New 实例化 LRUCache
func New(maxBytes int64, onEvicted func(string, Value)) *LRUCache {
	return &LRUCache{
		maxBytes:  maxBytes,
		ll:        list.New(),
		keyLink:     make(map[string]*list.Element),
		OnEvicted: onEvicted,
	}
}

// Get 查找功能；第一步是从字典中找到对应的双向链表的节点，第二步，将该节点移动到队首。
func (c *LRUCache) Get(key string) (Value, bool) {
	if ele, exist := c.keyLink[key]; exist {
		c.ll.MoveToFront(ele)
		kv := ele.Value.(*entry) // 断言
		return kv.value, true
	}
	return nil, false
}

// RemoveOldest 缓存淘汰。即移除最近最少访问的节点（队尾）
func (c *LRUCache) RemoveOldest() {
	if ele := c.ll.Back(); ele != nil {
		kv := ele.Value.(*entry)
		delete(c.keyLink, kv.key)                         // 删除字典中的key
		c.ll.Remove(ele)                                // 把元素从链表中删除
		c.nBytes -= kv.value.Len() + int64(len(kv.key)) // 更新已用内存

		// 如果回调函数 OnEvicted 不为 nil，则调用回调函数。
		if c.OnEvicted != nil {
			c.OnEvicted(kv.key, kv.value)
		}
	}
}

// Add 新增/修改
func (c *LRUCache) Add(key string, value Value) {
	// 如果键存在，则更新对应节点的值，并将该节点移到队首
	if ele, ok := c.keyLink[key]; ok {
		c.ll.MoveToFront(ele)
		kv := ele.Value.(*entry)
		c.nBytes += value.Len() - kv.value.Len()
		kv.value = value
	} else { // 不存在则新增
		ele = c.ll.PushFront(&entry{key, value})
		c.keyLink[key] = ele
		c.nBytes += int64(len(key)) + value.Len()
	}
	// 如果内存超过了设定的最大值maxBytes，则移除队尾节点直至内存不超
	for c.maxBytes != 0 && c.nBytes > c.maxBytes {
		c.RemoveOldest()
	}
}

func (c *LRUCache) Len() int {
	return c.ll.Len()
}
