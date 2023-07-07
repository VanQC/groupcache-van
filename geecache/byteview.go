package geecache

import "bytes"

/*
	抽象了一个只读数据结构 ByteView 用来表示缓存值
*/

// ByteView 只读数据结构，用来表示缓存值
type ByteView struct {
	b []byte // 选择 byte 类型是为了能够支持任意的数据类型的存储，例如字符串、图片等。
}

// Len 实现 lru 中 Value 接口
func (bv ByteView) Len() int64 {
	return int64(len(bv.b))
}

// ByteSlice 返回一个拷贝，防止缓存值被外部程序修改
func (bv ByteView) ByteSlice() []byte {
	return bytes.Clone(bv.b)
}

func (bv ByteView) String() string {
	return string(bv.b)
}
