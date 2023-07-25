package geecache

import (
	"strings"
	"testing"
)

func TestValidAddr(t *testing.T) {
	type test struct {
		addr string
		b    bool
	}
	tests := []test{
		{"127.0.0.1:8080", true},
		{"1277.0.0.1:8080", false},
		{"127.55.666.1:8080", false},
		{"255.255.255.255:8080", true},
		{"254.254.254.254:8080", true},
		{"255.256.255.255:8080", false},
	}
	for _, v := range tests {
		b := validPeerAddr(v.addr)
		if b != v.b {
			t.Errorf("test fail: %v", v.addr)
		}
	}
}

func Test_PeerRelation(t *testing.T) {
	addrMap := map[string]string{
		"8001": "127.0.0.1:8001",
		"8002": "127.0.0.1:8002",
		"8003": "127.0.0.1:8003",
	}
	var addrs []string
	for _, v := range addrMap {
		addrs = append(addrs, v)
	}
	s := NewServer("127.0.0.1:8001", 0, nil)
	s.SetPeers(addrs...)

	peer, ok := s.PickPeer("tom")
	if !ok {
		t.Errorf("PickPeer error")
	}
	if peer.(*client).name != "groupcache/127.0.0.1:8003" {
		t.Errorf("pick wrong peer")
	}

	peers := s.GetAll()
	for _, peer := range peers {
		add := peer.(*client).name
		ss := strings.Split(add, ":")
		if "groupcache/"+addrMap[ss[1]] != add {
			t.Errorf("GetAll error")
		}
	}
}
