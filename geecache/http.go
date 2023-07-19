package geecache

import (
	"bytes"
	"fmt"
	"geecache/consistenthash"
	pb "geecache/geecachepb"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBasePath = "/_geecache/"
	defaultReplicas = 50
)

type HTTPPool struct {
	self     string                  // 用来记录自己的地址，包括 主机名/IP 和 端口
	basePath string                  // 节点间通讯地址的前缀，默认是 /_geecache/
	replicas int                     // 一致性哈希时，key 翻倍的倍数。如果为空，则默认为 50
	hashFunc consistenthash.HashFunc // 指定哈希函数。若不指定则，则默认 crc32.ChecksumIEEE.

	mu      sync.Mutex
	peers   *consistenthash.ConsistHash
	clients map[string]*httpClient // keyed by e.g. "http://10.0.0.2:8008"
}

var httpPoolMade bool

func NewHTTPPool(self, basePath string, replicas int, hashFunc consistenthash.HashFunc) *HTTPPool {
	if httpPoolMade {
		panic("groupcache: NewHTTPPool must be called only once")
	}
	if basePath == "" {
		basePath = defaultBasePath
	}
	if replicas == 0 {
		replicas = defaultReplicas
	}

	p := &HTTPPool{
		self:     self,
		basePath: basePath,
		replicas: replicas,
		hashFunc: hashFunc,
		// peers and clients 会在 SetPeers() 中初始化，此函数不负责初始化
	}

	RegisterPeerPicker(p) // 将新建的 HTTPPool 注册到全局，不同的 Group 共享相同的 HTTP 池。
	// TODO
	// http.Handle(p.basePath, p)
	return p
}

func (p *HTTPPool) Log(formate string, v ...interface{}) {
	// Log 日志记录方法
	log.Printf("[Server %s] %s", p.self, fmt.Sprintf(formate, v...))
}

// SetPeers 更新 HTTPPool 中一致性哈希的节点，形成新的分布式节点
// 每个peer的值，都必须是有效的URL，例如：http://example.net:8000
func (p *HTTPPool) SetPeers(peers ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peers = consistenthash.New(p.replicas, p.hashFunc)
	p.peers.AddNode(peers...) // 添加节点

	p.clients = make(map[string]*httpClient, len(peers))
	for _, peer := range peers {
		p.clients[peer] = &httpClient{baseURL: peer + p.basePath} // 生成每个客户端的请求路径，每个请求路径都对应一个节点
	}
}

// PickPeer 封装了一致性哈希算法的 FindNode() 方法，根据具体的 key，选择节点，返回节点对应的 HTTP 客户端。
func (p *HTTPPool) PickPeer(key string) (ProtoGetter, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if peer := p.peers.FindNode(key); peer != "" && peer != p.self { //如果节点是自己，则会陷入循环。
		p.Log("Pick peer %s", peer)
		return p.clients[peer], true
	}
	return nil, false
}

func (p *HTTPPool) GetAll() (peers []ProtoGetter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	peers = make([]ProtoGetter, len(p.clients))
	i := 0
	for _, v := range p.clients {
		peers[i] = v
		i++
	}
	return
}

/*
	http.ListenAndServe(addr string, handler Handler) 用于实现一个服务端
	第一个参数是服务启动的地址，第二个参数是 Handler
	type Handler interface {
		ServeHTTP(w ResponseWriter, r *Request)
	}
	ServeHTTP 属于服务端方法，用来处理客户端的请求
*/

// ServeHTTP 实现 Handler 接口，用来处理客户端的请求。根据请求路径中的 groupName和key，定位数据组并查询key对应的数据
func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 解析请求
	if !strings.HasPrefix(r.URL.Path, p.basePath) { // 前缀不是 basePath 则panic
		panic("HTTPPool serving unexpected path" + r.URL.Path)
	}
	p.Log("[Method: %s] [Path: %s]", r.Method, r.URL.Path)

	// 请求路径格式: /basepath/groupname/key 。解析请求路径，获取参数
	parts := strings.SplitN(r.URL.Path[len(p.basePath):], "/", -1)
	if len(parts) != 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	groupName, key := parts[0], parts[1]
	group := GetGroup(groupName) // 找到数据组
	if group == nil {
		http.Error(w, "no such group: "+groupName, http.StatusNotFound)
		return
	}
	p.Log("解析路径成功，找到数据组group：%v", group.name)

	// 处理 Put 请求
	if r.Method == http.MethodPut {
		defer r.Body.Close()
		b := bufferPool.Get().(*bytes.Buffer)
		b.Reset()
		defer bufferPool.Put(b)
		if _, err := io.Copy(b, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 反序列化请求体中的信息，获取key，value，expire等信息
		var out pb.SetRequest
		if err := proto.Unmarshal(b.Bytes(), &out); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var expire time.Time
		if out.Expire != 0 {
			expire = time.Unix(out.Expire/int64(time.Second), out.Expire%int64(time.Second))
		}

		group.localSet(out.Key, out.Value, expire, &group.mainCache)
		return
	}

	// 处理 Delete 请求
	if r.Method == http.MethodDelete {
		group.localRemove(key)
		return
	}

	// 默认处理 GET 请求
	view, err := group.Query(key) // 查询数据组对应的缓存
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body, err := proto.Marshal(&pb.Response{Value: view.ByteSlice()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf") // 表示响应的内容是protobuf类型的数据
	w.Write(body)
}

// httpClient 只有一个字段——对等节点的地址，同时实现了 ProtoGetter 接口
type httpClient struct {
	baseURL string // 表示对等节点的访问地址，例如 http://127.0.0.1:8001/_geecache/
}

var bufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Get 方法，实现 ProtoGetter 接口
// 主要逻辑：向 http:// [basePath] /groupName/key 发送查询请求，将查询结果写入out中
// 例如 http://127.0.0.1:8001/_geecache/group01/Tom
func (hg *httpClient) Get(in *pb.Request, out *pb.Response) error {
	// in 中包含要查询的 groupName 和 key；out 用于写入查询的结果
	u := fmt.Sprintf("%v%v/%v",
		hg.baseURL,                     // 表示将要访问的远程节点的地址，例如 http://example.com/_geecache/
		url.QueryEscape(in.GetGroup()), // QueryEscape() 对传入参数进行转码使之可以安全的用在URL查询里。
		url.QueryEscape(in.GetKey()))

	resp, err := http.Get(u)
	if err != nil {
		log.Printf("请求%v时发生错误：%v", u, err.Error())
		return err
	} else {
		log.Printf("请求%v成功，节点运行中。", u)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned: %v", resp.Status)
	}

	// bytes, err := io.ReadAll(resp.Body)
	// 对于 Get 请求的回复体，可以采用上述方法进行读取回复信息，
	// 但对于高并发情况下，bytes可能存在频繁创建和销毁，即频繁进行内存分配和垃圾回收。

	// 通过使用缓冲池，可以避免频繁地创建和销毁 bytes.Buffer 对象，
	// 从而减少内存分配和垃圾回收的开销。这对于高并发或频繁地执行类似操作的场景中特别有用。
	b := bufferPool.Get().(*bytes.Buffer)
	b.Reset()
	defer bufferPool.Put(b)
	_, err = io.Copy(b, resp.Body)

	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}
	if err = proto.Unmarshal(b.Bytes(), out); err != nil {
		return fmt.Errorf("decoding response body: %v", err)
	}
	return nil
}

func (hg *httpClient) Set(in *pb.SetRequest) error {
	body, err := proto.Marshal(in)
	if err != nil {
		return fmt.Errorf("while marshaling SetRequest body: %w", err)
	}

	u := fmt.Sprintf("%v%v/%v",
		hg.baseURL,
		url.QueryEscape(in.GetGroup()),
		url.QueryEscape(in.GetKey()))

	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned: %v", resp.Status)
	}
	return nil
}

func (hg *httpClient) Remove(in *pb.Request) error {
	u := fmt.Sprintf("%v%v/%v",
		hg.baseURL,
		url.QueryEscape(in.GetGroup()),
		url.QueryEscape(in.GetKey()))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return err
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned: %v", resp.Status)
	}
	return nil
}
