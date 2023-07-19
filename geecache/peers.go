package geecache

import (
	pb "geecache/geecachepb"
)

/*
细化流程 ⑵：

使用一致性哈希选择节点        是                                    是
    |-----> 是否是远程节点 -----> HTTP 客户端访问远程节点 --> 成功？-----> 服务端返回返回值
                    |  否                                    ↓  否
                    |----------------------------> 回退到本地节点处理。
*/

// ProtoGetter 接口，每个对等节点（peer）都必须实现该接口。
type ProtoGetter interface {
	// Get 用于从对应 group 查找缓存值。ProtoGetter 就对应于上述流程中的 HTTP 客户端。
	Get(in *pb.Request, out *pb.Response) error

	Set(in *pb.SetRequest) error
	Remove(in *pb.Request) error
}

// PeerPicker 接口，实现根据传入的 key 选择相应节点 ProtoGetter 的功能
type PeerPicker interface {
	// PickPeer 返回 key 对应的节点，并返回 true，表明已指定远程peer。
	// 如果密钥所有者是当前对等方，则返回 nil 和 false。
	PickPeer(key string) (ProtoGetter, bool)

	// GetAll returns all the peers in the group
	GetAll() []ProtoGetter
}

type NoPeer struct{}

func (NoPeer) PickPeer(string) (peer ProtoGetter, ok bool) { return }
func (NoPeer) GetAll() (peers []ProtoGetter)               { return }

// 这部分做了简化，全局共用一个http池，即意味着 HTTPPool 只需注册一次
var portPicker PeerPicker

func RegisterPeerPicker(p PeerPicker) {
	if portPicker != nil {
		panic("RegisterPeerPicker called more than once")
	}
	portPicker = p
}

func getPeerPicker() PeerPicker {
	if portPicker == nil {
		return NoPeer{}
	}
	return portPicker
}
