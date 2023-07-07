package geecache

import (
	"fmt"
	"log"
	"testing"
)

var db = map[string]string{
	"Tom":  "630",
	"Jack": "589",
	"Sam":  "567",
}

func TestGet(t *testing.T) {
	loadCounts := make(map[string]int, len(db)) // 统计某个键调用回调函数的次数

	gp := NewGroup("scores", 2<<10, GetterFunc(
		func(key string) ([]byte, error) {
			log.Println("[slowDB] search key: " + key)
			if v, ok := db[key]; ok {
				//if _, exist := loadCounts[key]; !exist {}
				loadCounts[key] += 1
				return []byte(v), nil
			}
			return nil, fmt.Errorf("%s not exist", key)
		}))
	for k, v := range db {
		if btView, err := gp.Get(k); err != nil || btView.String() != v {
			t.Fatalf("fail to get value of %v", k)
		}
		if _, err := gp.Get(k); err != nil || loadCounts[k] > 1 {
			t.Fatalf("cache %s miss", k)
		}
		if view, err := gp.Get("unknown"); err == nil {
			t.Fatalf("the value of unknow should be empty, but %s got", view)
		}
	}
}
