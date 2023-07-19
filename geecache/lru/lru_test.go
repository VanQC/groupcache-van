package lru

import (
	//"geecache/lru"
	"testing"
	"time"
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
	lru := New(0, nil)
	da := date{"wqc", "123"}
	lru.Add("key1", da, time.Time{})
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

	lru := New(2, nil)
	lru.Add(k1, date{"w", "1"}, time.Time{})
	lru.Add(k2, date{"q", "2"}, time.Time{})
	lru.Add(k3, date{"c", "3"}, time.Time{})

	if _, ok := lru.Get("key1"); ok || lru.Len() != 2 {
		t.Fatalf("Removeoldest key1 failed")
	}
}

func TestRemove(t *testing.T) {
	lru := New(0, nil)
	lru.Add("myKey", 1234, time.Time{})
	if val, ok := lru.Get("myKey"); !ok {
		t.Fatal("TestRemove returned no match")
	} else if val != 1234 {
		t.Fatalf("TestRemove failed.  Expected %d, got %v", 1234, val)
	}

	lru.Remove("myKey")
	if _, ok := lru.Get("myKey"); ok {
		t.Fatal("TestRemove returned a removed entry")
	}
}

func TestExpire(t *testing.T) {
	var tests = []struct {
		name       string
		key        string
		expectedOk bool
		expire     time.Duration
		wait       time.Duration
	}{
		{"not-expired", "myKey", true, time.Second * 1, time.Duration(0)},
		{"expired", "expiredKey", false, time.Millisecond * 100, time.Millisecond * 150},
	}

	for _, tt := range tests {
		lru := New(0, nil)
		lru.Add(tt.key, 1234, time.Now().Add(tt.expire))
		time.Sleep(tt.wait)
		val, ok := lru.Get(tt.key)
		if ok != tt.expectedOk {
			t.Fatalf("%s: cache hit = %v; want %v", tt.name, ok, !ok)
		} else if ok && val != 1234 {
			t.Fatalf("%s expected get to return 1234 but got %v", tt.name, val)
		}
	}
}
