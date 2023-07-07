package lru

import (
	//"geecache/lru"
	"testing"
)

type date struct {
	name  string
	phone string
}

func (d date) Len() int64 {
	return int64(len(d.name + d.phone))
}

// 测试 FindNode 方法
func TestCache_Get(t *testing.T) {
	lru := New(int64(0), nil)
	da := date{"wqc", "123"}
	lru.Add("key1", da)
	if v, ok := lru.Get("key1"); !ok || v.(date) != da {
		t.Fatalf("cache hit key1 failed")
	}
	if _, ok := lru.Get("key2"); ok {
		t.Fatal("cache miss key2 failed")
	}
}

// 测试，当使用内存超过了设定值时，是否会触发“无用”节点的移除
func TestCache_RemoveOldest(t *testing.T) {
	k1, k2, k3 := "key1", "key2", "k3"

	lru := New(int64(12), nil)
	lru.Add(k1, date{"w", "1"})
	lru.Add(k2, date{"q", "2"})
	lru.Add(k3, date{"c", "3"})

	if _, ok := lru.Get("key1"); ok || lru.Len() != 2 {
		t.Fatalf("Removeoldest key1 failed")
	}
}
