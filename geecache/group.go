package geecache

import (
	"bytes"
	pb "geecache/geecachepb"
	"geecache/singleflight"
	"log"
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
	peersOnce sync.Once
	peers     PeerPicker

	cacheBytes int64 // limit for sum of mainCache and hotCache size

	// mainCache 是此进程（在其对等方中）具有权威性的键的缓存。
	// 不同数据的key根据一致性哈希原理，是分布在不同的节点上的，每个节点都是一个进程，
	// 也就是说节点的 mainCache 中只保存一致性哈希分配到自己的数据
	mainCache cache

	// hotCache 包含此对等节点不具有权威性的键值（否则它们将位于 mainCache 中），
	// 但足够流行，可以保证在此过程中进行镜像，以避免总是通过网络请求从对等方获取。
	// 拥有热缓存可避免网络热点，其中对等节点的网卡可能成为 热点数据 的瓶颈。
	// 此缓存谨慎使用，以最大化可全局存储的键值对总数。
	hotCache cache

	loader *singleflight.Set // 保证并发时相同请求只请求一次，返回相同结果
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
	if _, exist := groups[name]; exist {
		panic("duplicate registration of group " + name)
	}
	gp := &Group{
		name:       name,
		getter:     getter,
		cacheBytes: cacheBytes,

		// peers 通过调用 Get() 方法时执行一次
		// mainCache 延迟实例化

		loader: &singleflight.Set{},
	}
	groups[name] = gp
	return gp
}

// GetGroup 根据名称返回对应的group结构体
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

func (g *Group) initPeers() {
	if g.peers == nil {
		g.peers = getPeerPicker()
	}
}

// Query 根据key从 mainCache 中查找缓存，若存在则返回缓存值，若不存在，则调用 load 方法从外部查询key
func (g *Group) Query(key string) (ByteView, error) {
	// 初始化 group 的远程节点
	g.peersOnce.Do(g.initPeers)

	if key == "" {
		return ByteView{}, nil
	}
	if byteView, cacheHit := g.lookupCache(key); cacheHit {
		log.Println("cache hit")
		return byteView, nil
	}
	log.Println("cache not hit, get from load")
	return g.load(key)
}

// 从外部查询 key
func (g *Group) load(key string) (ByteView, error) {
	// 防止缓存击穿。并发查询请求只执行一次
	// 确保了并发场景下针对相同的 key，load 过程只会调用一次
	btView, err := g.loader.Do(key, func() (interface{}, error) {
		// 再次查缓存
		if value, cacheHit := g.lookupCache(key); cacheHit {
			return value, nil
		}
		// 查远程节点，若存在的话。
		if g.peers != nil {
			if peer, ok := g.peers.PickPeer(key); ok {
				log.Println("cache not hit, get from peer")
				if value, err := g.getFromPeer(peer, key); err == nil {
					log.Println("从远程节点成功获取数据")
					return value, nil
				}
			}
		}
		// 查本地
		return g.queryLocally(key)
	})
	if err == nil {
		return btView.(ByteView), nil
	}
	return ByteView{}, err
}

// 从两个缓存中查找
func (g *Group) lookupCache(key string) (value ByteView, ok bool) {
	if g.cacheBytes <= 0 {
		return
	}
	value, ok = g.mainCache.get(key)
	if ok {
		return
	}
	value, ok = g.hotCache.get(key)
	return
}

// 访问远程节点，获取缓存值
func (g *Group) getFromPeer(peer ProtoGetter, key string) (ByteView, error) {
	request := &pb.Request{
		Group: g.name,
		Key:   key,
	}
	response := &pb.Response{}
	if err := peer.Get(request, response); err != nil {
		return ByteView{}, err
	}
	value := ByteView{b: response.Value}

	// TODO 这里把热点数据加入hotCache 的策略有待进一步优化，这里采取每次都加入
	g.populateCache(key, value, &g.hotCache)
	return value, nil
}

// 调用回调函数 g.getter.Get() 从其他地方获取源数据，
// 并将源数据添加到缓存 mainCache 中（通过 populateCache 方法）
func (g *Group) queryLocally(key string) (ByteView, error) {
	b, err := g.getter.Get(key)
	if err != nil {
		return ByteView{}, err
	}
	value := ByteView{bytes.Clone(b)}
	g.populateCache(key, value, &g.mainCache) // 将获取到的源数据添加到缓存 mainCache 中
	return value, nil
}

// 根据传入的 cache 参数确定是 hotCache 还是 mainCache，将 key value 存入 cache 中。
func (g *Group) populateCache(key string, value ByteView, cache *cache) {
	if g.cacheBytes <= 0 {
		return
	}
	cache.add(key, value)
	for {
		mainBytes, hotBytes := g.mainCache.bytes(), g.hotCache.bytes()
		if mainBytes+hotBytes <= g.cacheBytes {
			return
		}
		if hotBytes > mainBytes/8 {
			(&g.hotCache).removeOldest()
		} else {
			(&g.mainCache).removeOldest()
		}
	}
}
