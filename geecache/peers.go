package geecache

import pb "project_cache/geecache/geecachepb"

// 根据传入的 key 选择相应节点 PeerGetter。
type PeerPicker interface {
	PickPeer(key string) (PeerGetter, bool)
}

/*
细化流程 ⑵：


使用一致性哈希选择节点        是                                    是
    |-----> 是否是远程节点 -----> HTTP 客户端访问远程节点 --> 成功？-----> 服务端返回返回值
                    |  否                                    ↓  否
                    |----------------------------> 回退到本地节点处理。
*/

// 用于从对应 group 查找缓存值。PeerGetter 就对应于上述流程中的 HTTP 客户端。
type PeerGetter interface {
	PeerGet(in *pb.Request, out *pb.Response) error
}

//type PeerGetter interface {
//	PeerGet(group, key string) ([]byte, error)
//}
