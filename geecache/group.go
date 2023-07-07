package geecache

import (
	"bytes"
	"log"
	"project_cache/geecache/singleflight"
	"sync"
)

// Getter 回调。当缓存不存在时，调用这个函数，得到源数据
type Getter interface {
	Get(key string) ([]byte, error)
}

// GetterFunc 函数类型 实现Getter接口
type GetterFunc func(key string) ([]byte, error)

// Get 调用函数自身，实现 Getter 接口
func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}

/*
	Group 是 GeeCache 最核心的数据结构，负责与用户的交互，并且控制缓存值存储和获取的流程。
	+-----------------------------------------------------------------------------------+
	|							是													  	|
	| 接收 key --> 检查是否被缓存 -----> 返回缓存值 ⑴										|
	| 				|  否                         是										|
	| 				|-----> 是否应当从远程节点获取 -----> 与远程节点交互 --> 返回缓存值 ⑵		|
	| 							|  否													|
	| 							|-----> 调用`回调函数`，获取值并添加到缓存 --> 返回缓存值 ⑶		|
	+-----------------------------------------------------------------------------------+
*/

// Group 封装了回调函数和 cache 结构体作为一个缓存数据组
type Group struct {
	name      string
	getter    Getter // 回调函数
	mainCache cache  // 缓存
	peers     PeerPicker
	loader    *singleflight.Set // 保证并发时相同请求只请求一次，返回相同结果
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group) // 存储全部的 Group 结构体
)

// NewGroup 实例化 Group，并且将其存储在全局变量 groups 中
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock() // 防止同时修改同一条group实例（name相同的）
	defer mu.Unlock()
	gp := &Group{
		name:      name,
		getter:    getter,
		mainCache: cache{cacheBytes: cacheBytes}, // 此处不用实例化lru，因为采用延迟初始化。
		loader:    &singleflight.Set{},
	}
	groups[name] = gp
	return gp
}

// GetGroup 根据名称返回对应的group结构体
func GetGroup(name string) *Group {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := groups[name]; ok {
		return v
	}
	return nil
}

// RegisterPeersPicker 将实现了 PeerPicker 接口的 HTTPPool 注入到 Group 中
func (g *Group) RegisterPeersPicker(peers PeerPicker) {
	if g.peers != nil {
		panic("RegisterPeerPicker called more than once")
	}
	g.peers = peers
}

// QueryCache 根据key从 mainCache 中查找缓存，若存在则返回缓存值，若不存在，则调用 load 方法从外部查询key
func (g *Group) QueryCache(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, nil
	}
	if byteView, ok := g.mainCache.get(key); ok {
		log.Println("cache hit")
		return byteView, nil
	}
	log.Println("cache not hit, get from load")
	return g.load(key)
}

// 从外部查询 key
func (g *Group) load(key string) (ByteView, error) {
	// 确保了并发场景下针对相同的 key，load 过程只会调用一次
	btView, err := g.loader.Do(key, func() (interface{}, error) {
		if g.peers != nil { // 存在远程节点
			if peerGetter, ok := g.peers.PickPeer(key); ok {
				if value, err := g.getFromPeer(peerGetter, key); err == nil {
					return value, nil
				}
			}
		}
		return g.queryLocally(key)
	})
	if err == nil {
		return btView.(ByteView), nil
	}

	return ByteView{}, err
	//return btView.(ByteView), err
}

// 访问远程节点，获取缓存值
func (g *Group) getFromPeer(peer PeerGetter, key string) (ByteView, error) {
	if btView, err := peer.PeerGet(g.name, key); err == nil {
		return ByteView{b: btView}, nil
	} else {
		return ByteView{}, err
	}
}

// 调用回调函数 g.getter.Get() 从其他地方获取源数据，
// 并将源数据添加到缓存 mainCache 中（通过 populateCache 方法）
func (g *Group) queryLocally(key string) (ByteView, error) {
	b, err := g.getter.Get(key)
	if err != nil {
		return ByteView{}, err
	}
	value := ByteView{bytes.Clone(b)}
	//g.populateCache(key,value)
	g.mainCache.add(key, value) // 将获取到的源数据添加到缓存 mainCache 中
	return value, nil
}

//func (g *Group) populateCache(key string, value ByteView) {
//	g.mainCache.add(key, value)
//}
