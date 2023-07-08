package geecache

import (
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"net/http"
	"net/url"
	"project_cache/geecache/consistenthash"
	pb "project_cache/geecache/geecachepb"
	"strings"
	"sync"
)

const (
	defaultBasePath = "/_geecache/"
	defaultReplicas = 50
)

// 创建 HTTP 客户端类 httpGetter，实现 PeerGetter 接口
// httpClient
type httpClient struct {
	baseURL string // 将要访问的远程节点的地址，例如 http://example.com/_geecache/。
}

// PeerGet 客户端有各自的baseURL，
func (hg *httpClient) PeerGet(in *pb.Request, out *pb.Response) error {

	u := fmt.Sprintf("%v%v/%v",
		hg.baseURL,                     // 表示将要访问的远程节点的地址，例如 http://example.com/_geecache/
		url.QueryEscape(in.GetGroup()), // QueryEscape函数对传入参数进行转码使之可以安全的用在URL查询里。
		url.QueryEscape(in.GetKey()))

	resp, err := http.Get(u)
	if err != nil {
		log.Printf("请求%v时发生错误：%v", u, err.Error())
		return err
	} else {
		log.Printf("请求%v时，节点在开启", u)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned: %v", resp.Status)
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}
	if err = proto.Unmarshal(bytes, out); err != nil {
		return fmt.Errorf("decoding response body: %v", err)
	}

	return nil
}

//func (hg *httpClient) PeerGet(group, key string) ([]byte, error) {
//
//	u := fmt.Sprintf("%v%v/%v",
//		hg.baseURL,             // 表示将要访问的远程节点的地址，例如 http://example.com/_geecache/
//		url.QueryEscape(group), // QueryEscape函数对传入参数进行转码使之可以安全的用在URL查询里。
//		url.QueryEscape(key))
//
//	resp, err := http.Get(u)
//	if err != nil {
//		log.Printf("请求%v时发生错误：%v", u, err.Error())
//		return nil, err
//	} else {
//		log.Printf("请求%v时，节点在开启", u)
//	}
//	defer resp.Body.Close()
//
//	if resp.StatusCode != 200 {
//		return nil, fmt.Errorf("server returned: %v", resp.Status)
//	}
//
//	bytes, err := io.ReadAll(resp.Body)
//	if err != nil {
//		return nil, fmt.Errorf("reading response body: %v", err)
//	}
//
//	return bytes, nil
//}

type HTTPPool struct {
	self     string // 用来记录自己的地址，包括 主机名/IP 和 端口
	basePath string // 节点间通讯地址的前缀，默认是 /_geecache/
	mu       sync.Mutex
	peers    *consistenthash.ConsistHash
	clients  map[string]*httpClient
}

func NewHTTPPool(self string) *HTTPPool {
	return &HTTPPool{
		self:     self,
		basePath: defaultBasePath,
	}
}

// Log 日志记录方法
func (p *HTTPPool) Log(formate string, v ...interface{}) {
	log.Printf("[Server %s] %s", p.self, fmt.Sprintf(formate, v...))
}

/*
	http.ListenAndServe() 用于实现一个服务端
	http.ListenAndServe() 接收 2 个参数，第一个参数是服务启动的地址，第二个参数是 Handler
	type Handler interface {
		ServeHTTP(w ResponseWriter, r *Request)
	}
	ServeHTTP 属于服务端方法，用来处理客户端的请求
*/

// ServeHTTP 实现 Handler 接口，用来处理客户端的请求。根据请求路径中的 groupname和key，定位数据组并查询key对应的数据
func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 前缀不是 basepath 则panic
	if !strings.HasPrefix(r.URL.Path, p.basePath) {
		panic("HTTPPool serving unexpected path" + r.URL.Path)
	}
	p.Log("[Method: %s] [Path: %s]", r.Method, r.URL.Path)
	// require path: /basepath/groupname/key
	// 解析请求路径
	parts := strings.SplitN(r.URL.Path[len(p.basePath):], "/", -1)
	if len(parts) != 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	groupName, key := parts[0], parts[1]
	group := GetGroup(groupName) // 找到数据组
	p.Log("解析路径成功，找到数据组group：%v", group)
	if group == nil {
		http.Error(w, "no such group: "+groupName, http.StatusNotFound)
		return
	}
	view, err := group.QueryCache(key) // 查询数据组对应的缓存
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body, err := proto.Marshal(&pb.Response{Value: view.ByteSlice()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(body)
}

// SetPeers 实例化了一致性哈希算法，并且添加了传入的节点。
func (p *HTTPPool) SetPeers(peers ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peers = consistenthash.New(defaultReplicas, nil)
	p.peers.AddNode(peers...) // 添加节点

	p.clients = make(map[string]*httpClient, len(peers))
	for _, peer := range peers {
		p.clients[peer] = &httpClient{baseURL: peer + p.basePath} // 生成每个客户端的请求路径，每个请求路径都对应一个节点
	}

}

// PickPeer 封装了一致性哈希算法的 FindNode() 方法，根据具体的 key，选择节点，返回节点对应的 HTTP 客户端。
func (p *HTTPPool) PickPeer(key string) (PeerGetter, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if peer := p.peers.FindNode(key); peer != "" && peer != p.self { //如果节点是自己，则会陷入循环。整个体系实有点问题的，所以加这段
		p.Log("Pick peer %s", peer)
		return p.clients[peer], true
	}
	return nil, false
}
