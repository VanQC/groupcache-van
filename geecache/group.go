package geecache

import (
	"bytes"
	"errors"
	pb "geecache/geecachepb"
	"geecache/singleflight"
	"log"
	"sync"
	"time"
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

	// 保证并发时相同请求只请求一次，返回相同结果
	loadGroup *singleflight.Set

	// 确保无论并发调用方的数量如何，仅远程添加一次key
	setGroup *singleflight.Set

	// 确保无论并发调用方的数量如何，仅远程移除一次key
	removeGroup *singleflight.Set
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

		loadGroup:   &singleflight.Set{},
		setGroup:    &singleflight.Set{},
		removeGroup: &singleflight.Set{},
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
	btView, err := g.loadGroup.Do(key, func() (interface{}, error) {
		// 再次查缓存
		if value, cacheHit := g.lookupCache(key); cacheHit {
			return value, nil
		}
		// 查远程节点，若存在的话。
		if g.peers != nil {
			if peer, ok := g.peers.PickPeer(key); ok {
				log.Printf("cache not hit, get from peer--[%v]\n", peer.(*client).name)
				if value, err := g.getFromPeer(peer, key); err == nil {
					log.Printf("从远程节点[%v]成功获取数据", peer.(*client).name)
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
	value := ByteView{bytes.Clone(b), time.Time{}} // 空结构体表示零值，默认不过期
	g.populateCache(key, value, &g.mainCache)      // 将获取到的源数据添加到缓存 mainCache 中
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

// Set 向对应节点的缓存中加入 key value
func (g *Group) Set(key string, value []byte, expire time.Time, isHotCache bool) error {
	g.peersOnce.Do(g.initPeers)
	if key == "" {
		return errors.New("empty Set() key not allowed")
	}

	_, err := g.setGroup.Do(key, func() (interface{}, error) {
		// if remote peer owns this key
		if peer, ok := g.peers.PickPeer(key); ok {
			if err := g.setFromPeer(peer, key, value, expire); err != nil {
				return nil, err
			}
			if isHotCache {
				g.localSet(key, value, expire, &g.hotCache)
			}
			return nil, nil
		}
		// else we own this key
		g.localSet(key, value, expire, &g.mainCache)
		return nil, nil
	})
	return err
}

func (g *Group) setFromPeer(peer ProtoGetter, key string, value []byte, expire time.Time) error {
	var e int64
	if !expire.IsZero() {
		e = expire.UnixNano()
	}

	req := &pb.SetRequest{
		Group:  g.name,
		Key:    key,
		Value:  value,
		Expire: e,
	}
	return peer.Set(req)
}

func (g *Group) localSet(key string, value []byte, expire time.Time, cache *cache) {
	if g.cacheBytes <= 0 {
		return
	}
	btv := ByteView{
		b: value,
		e: expire,
	}
	// 在g.loadGroup.Do() 执行期间，会进行缓存的增/改；在执行 localRemove 操作时也会进行缓存的删除，
	// 加上这里的增加缓存操作，这三者之间不能与之并发进行，只有能获取到锁的一方才能执行，其他等待。
	g.loadGroup.Lock(func() {
		g.populateCache(key, btv, cache)
	})
}

// Remove 向对应节点的缓存中移除 key value
func (g *Group) Remove(key string) error {
	g.peersOnce.Do(g.initPeers)
	if key == "" {
		return errors.New("empty Remove() key not allowed")
	}

	_, err := g.removeGroup.Do(key, func() (interface{}, error) {
		// Remove from key owner first
		owner, ok := g.peers.PickPeer(key)
		if ok {
			if err := g.removeFromPeer(key, owner); err != nil {
				return nil, err
			}
		}
		// Remove from our cache next
		g.localRemove(key)

		// 异步清除其他节点中所有的 hot and main caches
		wg := sync.WaitGroup{}
		errs := make(chan error)
		for _, peer := range g.peers.GetAll() {
			if peer == owner {
				continue
			}
			wg.Add(1)
			go func(peer ProtoGetter) {
				errs <- g.removeFromPeer(key, peer)
				wg.Done()
			}(peer)
		}
		go func() {
			wg.Wait()
			close(errs)
		}()
		// TODO(thrawn01): Should we report all errors? Reporting context
		//  cancelled error for each peer doesn't make much sense.
		var err error
		for e := range errs {
			err = e
		}
		return nil, err
	})
	return err
}

func (g *Group) removeFromPeer(key string, peer ProtoGetter) error {
	req := &pb.Request{
		Group: g.name,
		Key:   key,
	}
	return peer.Remove(req)
}

func (g *Group) localRemove(key string) {
	// Clear key from our local cache
	if g.cacheBytes <= 0 {
		return
	}

	// 在g.loadGroup.Do() 执行期间，会进行缓存的增/改；在执行 localSet 操作时也会进行缓存的删除，
	// 加上这里的删除缓存操作，这三者之间不能与之并发进行，只有能获取到锁的一方才能执行，其他等待。
	g.loadGroup.Lock(func() {
		g.hotCache.remove(key)
		g.mainCache.remove(key)
	})
}
