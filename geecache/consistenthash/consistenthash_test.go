package consistenthash

import (
	"strconv"
	"testing"
)

/*
如果要进行测试，那么我们需要明确地知道每一个传入的 key 的哈希值，那使用默认的 crc32.ChecksumIEEE 算法显然达不到目的。
所以在这里使用了自定义的 Hash 算法。自定义的 Hash 算法只处理数字，传入字符串表示的数字，返回对应的数字即可。

一开始，有 2/4/6 三个真实节点，对应的虚拟节点的哈希值是 02/12/22、04/14/24、06/16/26。
那么用例 2/11/23/27 选择的虚拟节点分别是 02/12/24/02，也就是真实节点 2/2/4/2。
添加一个真实节点 8，对应虚拟节点的哈希值是 08/18/28，此时，用例 27 对应的虚拟节点从 02 变更为 28，即真实节点 8。
*/
func TestHashing(t *testing.T) {
	hash := New(3, func(data []byte) uint32 {
		num, err := strconv.Atoi(string(data))
		if err != nil {
			panic("类型转换错误")
		}
		return uint32(num)
	})
	// Given the above hash function, this will give replicas with "hashes":
	// 2, 4, 6, 12, 14, 16, 22, 24, 26
	hash.AddNode("6", "4", "2")

	testCase := map[string]string{
		"2":  "2",
		"11": "2",
		"23": "4",
		"27": "2",
	}
	for k, v := range testCase {
		if hash.FindNode(k) != v {
			t.Errorf("asking for %s, should be %s", k, v)
		}
	}
	hash.AddNode("8")
	testCase["27"] = "8" // 27 should now map to 8.
	for k, v := range testCase {
		if hash.FindNode(k) != v {
			t.Errorf("asking for %s, should be %s", k, v)
		}
	}
}
