package lru

import (
	"container/list"
	"time"
)

type NowFunc func() time.Time

type LRUCache struct {
	maxEntries int                      // 允许的最大缓存条目数。零表示没有限制
	ll         *list.List               // 直接使用 Go 语言标准库实现的双向链表list.List
	keyLink    map[string]*list.Element // 键是字符串，值是双向链表中对应节点的指针

	// 某条记录从缓存中被移除时的回调函数，可以为 nil
	OnEvicted func(key string, value interface{})

	// Now is the Now() function the cache will use to determine
	// the current time which is used to calculate expired values
	// Defaults to time.Now()
	Now NowFunc
}

type entry struct {
	key    string
	value  interface{}
	expire time.Time // 0 表示永不过期
}

//type Value interface {
//	Len() int64 // 用于返回值所占用的内存大小
//}

// New 实例化 LRUCache
func New(maxEntries int, onEvicted func(string, interface{})) *LRUCache {
	return &LRUCache{
		maxEntries: maxEntries,
		ll:         list.New(),
		keyLink:    make(map[string]*list.Element),
		OnEvicted:  onEvicted,
		Now:        time.Now,
	}
}

// Get 查找功能：第一步从字典中找到对应的双向链表的节点，判断是否过期
// 若过期，则进行删除操作；不过期，则将该节点移动到队首。
func (c *LRUCache) Get(key string) (interface{}, bool) {
	if c.keyLink == nil {
		return nil, false
	}
	if ele, exist := c.keyLink[key]; exist {
		kv := ele.Value.(*entry)                              // 断言
		if !kv.expire.IsZero() && kv.expire.Before(c.Now()) { // 判断是否过期
			c.removeElement(ele)
			return nil, false
		}
		c.ll.MoveToFront(ele)
		return kv.value, true
	}
	return nil, false
}

// Add 新增/修改
func (c *LRUCache) Add(key string, value interface{}, expire time.Time) {

	// 由于本方法只在cache.go 中的 add() 方法中被调用，
	// add() 方法在前面已经判断 LRUCache 是否为nil，是 nil 则调用 New() 初始化，
	// 所以此处不需要再判断 keyLink 和 ll 是否为nil

	// 如果键存在，则更新对应节点的值，并将该节点移到队首
	if ele, ok := c.keyLink[key]; ok {
		c.ll.MoveToFront(ele)
		kv := ele.Value.(*entry)
		kv.value = value
		kv.expire = expire
		return
	}
	// 不存在则新增
	ele := c.ll.PushFront(&entry{key, value, expire})
	c.keyLink[key] = ele
	// 如果键值对数量超过了设定的最大值maxEntries，则移除队尾节点直至不超
	for c.maxEntries != 0 && c.Len() > c.maxEntries {
		c.RemoveOldest()
	}
}

// Remove 移除指定 key
func (c *LRUCache) Remove(key string) {
	if c.keyLink == nil {
		return
	}
	if ele := c.keyLink[key]; ele != nil {
		c.removeElement(ele)
	}
}

// RemoveOldest 缓存淘汰。即移除最近最少访问的节点（队尾）
func (c *LRUCache) RemoveOldest() {
	if c.keyLink == nil {
		return
	}
	if ele := c.ll.Back(); ele != nil {
		c.removeElement(ele)
	}
}

func (c *LRUCache) removeElement(ele *list.Element) {
	kv := ele.Value.(*entry)
	delete(c.keyLink, kv.key) // 删除字典中的key
	c.ll.Remove(ele)          // 把元素从链表中删除

	// 如果回调函数 OnEvicted 不为 nil，则调用回调函数。
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value)
	}
}

func (c *LRUCache) Len() int {
	return c.ll.Len()
}
