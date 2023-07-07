package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
)

// HashFunc 函数类型：将 []byte 数据哈希映射成 uint32
type HashFunc func(data []byte) uint32

type ConsistHash struct {
	hashFunc HashFunc       // 哈希函数
	replicas int            // 虚拟节点倍数
	hashRing []int          // 虚拟节点构成的哈希环
	hashMap  map[int]string // 虚拟节点与真实节点的映射表
}

// New 允许自定义虚拟节点倍数和 HashFunc 函数。
func New(replicas int, hashFunc HashFunc) *ConsistHash {
	if hashFunc == nil {
		hashFunc = crc32.ChecksumIEEE // 返回数据的 CRC-32 校验和
	}
	return &ConsistHash{
		hashFunc: hashFunc,
		replicas: replicas,
		hashRing: nil,
		hashMap:  make(map[int]string),
	}
}

// AddNode 允许传入 0 或 多个真实节点的名称
func (ch *ConsistHash) AddNode(nodeNames ...string) {
	for _, nodeName := range nodeNames {
		for i := 0; i < ch.replicas; i++ {
			hashValue := int(ch.hashFunc([]byte(strconv.Itoa(i) + nodeName))) // 计算虚拟节点的哈希值
			ch.hashRing = append(ch.hashRing, hashValue)                      // 将虚拟节点哈希值保存起来
			ch.hashMap[hashValue] = nodeName                                  // 构建每个节点对应的虚拟节点哈希值与节点名的映射
		}
	}
	sort.Ints(ch.hashRing) // 环上的哈希值排序
}

// FindNode 获取真实节点
func (ch *ConsistHash) FindNode(key string) string {
	length := len(ch.hashRing)
	if length == 0 {
		return ""
	}
	hashValue := int(ch.hashFunc([]byte(key)))
	index := sort.Search(length, func(i int) bool {
		return ch.hashRing[i] >= hashValue // 找到第一个大于它的虚拟节点
	})
	//k := ch.hashRing[index%len(ch.hashRing)]
	if index == length {
		index = 0
	}
	return ch.hashMap[ch.hashRing[index]]
}
