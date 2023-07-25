package geecache

import (
	"context"
	"fmt"
	"geecache/consistenthash"
	pb "geecache/geecachepb"
	"geecache/registry"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	defaultAddr     = "127.0.0.1:8090"
	defaultReplicas = 50
)

type Server struct {
	pb.UnimplementedGroupCacheServer

	addr       string     // 用来记录自己的地址，format: ip:port
	status     bool       // true: running false: stop
	stopSignal chan error // 通知registry revoke服务

	replicas int                     // 一致性哈希时，key 翻倍的倍数。如果为空，则默认为 50
	hashFunc consistenthash.HashFunc // 指定哈希函数。若不指定则，则默认 crc32.ChecksumIEEE.

	mu      sync.Mutex
	peers   *consistenthash.ConsistHash
	clients map[string]*client // keyed by e.g. "http://10.0.0.2:8008"
}

var serverMade bool

func NewServer(addr string, replicas int, hashFunc consistenthash.HashFunc) *Server {
	if serverMade {
		panic("groupcache: NewServer must be called only once")
	}
	if addr == "" {
		addr = defaultAddr
	}
	if replicas == 0 {
		replicas = defaultReplicas
	}
	s := &Server{
		addr:     addr,
		replicas: replicas,
		hashFunc: hashFunc,
		// peers and clients 会在 SetPeers() 中初始化，此函数不负责初始化
	}

	RegisterPeerPicker(s) // 将新建的 Server 注册到全局，不同的 Group 共享相同的 Server 池。
	return s
}

func (s *Server) Log(formate string, v ...interface{}) {
	// Log 日志记录方法
	log.Printf("[Server %s] %s", s.addr, fmt.Sprintf(formate, v...))
}

// SetPeers 更新 Server 中一致性哈希的节点，形成新的分布式节点
// 每个peer的name，都必须是有效的groupcache/ip+port，例如：groupcache/127.0.0.1:8000
func (s *Server) SetPeers(peers ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = consistenthash.New(s.replicas, s.hashFunc)
	s.peers.AddNode(peers...) // 添加节点

	s.clients = make(map[string]*client, len(peers))
	for _, peer := range peers {
		if !validPeerAddr(peer) {
			panic(fmt.Sprintf("[peer %s] invalid address format, it should be x.x.x.x:port", peer))
		}
		s.clients[peer] = &client{name: "groupcache/" + peer} // 生成每个客户端的请求路径，每个请求路径都对应一个节点
	}
}

// PickPeer 封装了一致性哈希算法的 FindNode() 方法，根据具体的 key，选择节点，返回节点对应的 HTTP 客户端。
func (s *Server) PickPeer(key string) (ProtoGetter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if peer := s.peers.FindNode(key); peer != "" && peer != s.addr { //如果节点是自己，则会陷入循环。
		s.Log("Pick peer %s", peer)
		return s.clients[peer], true
	}
	return nil, false
}

func (s *Server) GetAll() (peers []ProtoGetter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peers = make([]ProtoGetter, len(s.clients))
	i := 0
	for _, v := range s.clients {
		peers[i] = v
		i++
	}
	return
}

func (s *Server) Get(ctx context.Context, in *pb.Request) (*pb.Response, error) {
	groupName, key := in.GetGroup(), in.GetKey()
	out := &pb.Response{}
	group := GetGroup(groupName) // 找到数据组
	if group == nil {
		return out, fmt.Errorf("no such group: " + groupName)
	}
	s.Log("执行Get中找到数据组group：%v", group.name)

	view, err := group.Query(key) // 查询数据组对应的缓存
	if err != nil {
		return out, fmt.Errorf(err.Error())
	}

	out.Value, err = proto.Marshal(&pb.Response{Value: view.ByteSlice()})
	if err != nil {
		return out, fmt.Errorf(err.Error())
	}
	return out, nil
}

func (s *Server) Put(ctx context.Context, in *pb.SetRequest) (*emptypb.Empty, error) {
	group := GetGroup(in.GetGroup())
	if group == nil {
		return new(emptypb.Empty), fmt.Errorf("no such group: " + in.GetGroup())
	}
	s.Log("执行Put中找到数据组group：%v", group.name)

	var expire time.Time
	if in.Expire != 0 {
		expire = time.Unix(in.Expire/int64(time.Second), in.Expire%int64(time.Second))
	}

	group.localSet(in.Key, in.Value, expire, &group.mainCache)
	return new(emptypb.Empty), nil
}

func (s *Server) Delete(ctx context.Context, in *pb.Request) (*emptypb.Empty, error) {
	group := GetGroup(in.GetGroup())
	if group == nil {
		return new(emptypb.Empty), fmt.Errorf("no such group: " + in.GetGroup())
	}
	s.Log("执行Delete中找到数据组group：%v", group.name)

	group.localRemove(in.GetKey())
	return new(emptypb.Empty), nil
}

// -----------------启动服务----------------------
// 1. status == true 表示服务器已在运行
// 2. 初始化stop channel, 用于通知registry stop keep alive
// 3. 初始化tcp socket并开始监听
// 4. 注册rpc服务至grpc 这样grpc收到request可以分发给server处理
// 5. 将 [服务名/ip:port] 注册至etcd 这样client可以通过etcd
//    获取服务地址，从而进行通信。这样的好处是client只需知道服务名
//    以及etcd的Host即可获取对应服务IP，无需写死至client代码中
// ----------------------------------------------

func (s *Server) Start() error {
	s.mu.Lock()
	if s.status == true {
		s.mu.Unlock()
		return fmt.Errorf("server already started")
	}
	s.status = true
	s.stopSignal = make(chan error)

	if !validPeerAddr(s.addr) {
		panic(fmt.Sprintf("[%s] is invalid address format, it should be x.x.x.x:port", s.addr))
	}
	port := strings.Split(s.addr, ":")[1]
	// 监听本地的port端口
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	gs := grpc.NewServer()             // 创建gRPC服务器
	pb.RegisterGroupCacheServer(gs, s) // 在gRPC服务端注册服务

	// 将服务注册至 etcd
	go func() {
		// Register never return unless stop signal received
		erro := registry.RegisterServiceToETCD("groupcache", s.addr, s.stopSignal)
		if erro != nil {
			log.Fatalf(erro.Error())
		}
		close(s.stopSignal) // Close channel
		if erro = lis.Close(); erro != nil {
			log.Fatalf(erro.Error())
		} // Close tcp listen

		log.Printf("[%s] Revoke service and close tcp socket ok.", s.addr)
	}()

	s.mu.Unlock()
	// 启动grpc服务
	if err = gs.Serve(lis); s.status && err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}
	return nil
}

// 判断是否满足 x.x.x.x:port 的格式
func validPeerAddr(addr string) bool {
	ss := strings.Split(addr, ":")
	if len(ss) != 2 {
		return false
	}
	if ss[0] != "localhost" && net.ParseIP(ss[0]) == nil {
		return false
	}
	return true
}
