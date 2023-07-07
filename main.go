package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"project_cache/geecache"
)

var db = map[string]string{
	"Tom":  "630",
	"Jack": "589",
	"Sam":  "567",
}

func creatGroup() *geecache.Group {
	return geecache.NewGroup("scores", 2<<10, geecache.GetterFunc(
		func(key string) ([]byte, error) {
			log.Println("[slowDB] search key: " + key)
			if v, ok := db[key]; ok {
				return []byte(v), nil
			}
			return nil, fmt.Errorf("%s not exist", key)
		}))
}

func startCacheServer(addr string, peers []string, gp *geecache.Group) {

	htpPol := geecache.NewHTTPPool(addr)
	htpPol.SetPeers(peers...)
	gp.RegisterPeersPicker(htpPol)
	log.Println("geecache is running at", addr)
	log.Fatal(http.ListenAndServe(addr[7:], htpPol)) // 7 的意思是去掉前缀 "http://"
}

func startAPIServer(apiAddr string, gp *geecache.Group) {
	http.Handle("/api", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			key := r.URL.Query().Get("key")
			view, err := gp.QueryCache(key)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(view.ByteSlice())
		}))
	log.Println("fontend server is running at", apiAddr)
	log.Fatal(http.ListenAndServe(apiAddr[7:], nil))
}

func main() {

	var (
		port int
		api  bool
	)
	flag.IntVar(&port, "port", 8001, "Geecache server port")
	flag.BoolVar(&api, "api", false, "Start a api server?")
	flag.Parse()

	fmt.Println(port, api)
	//返回命令行参数后的其他参数
	fmt.Println("命令行参数后的其他参数:", flag.Args())
	//返回使用的命令行参数个数
	fmt.Println("使用的命令行参数个数", flag.NFlag())

	apiAddr := "http://localhost:9999"
	addrMap := map[int]string{
		8001: "http://localhost:8001",
		8002: "http://localhost:8002",
		8003: "http://localhost:8003",
	}
	var addrs []string
	for _, v := range addrMap {
		addrs = append(addrs, v)
	}
	group := creatGroup()
	if api {
		go startAPIServer(apiAddr, group)
	}
	startCacheServer(addrMap[port], addrs, group)
}

// Overall flow char										     requests					        local
// gee := createGroup() --------> /api Service : 9999 ------------------------------> gee.Get(key) ------> g.mainCache.Get(key)
// 						|											^					|
// 						|											|					|remote
// 						v											|					v
// 				cache Service : 800x								|			g.peers.PickPeer(key)
// 						|create hash ring & init peerGetter			|					|
// 						|registry peers write in g.peer				|					|p.httpGetters[p.hashRing(key)]
// 						v											|					|
//			httpPool.Set(otherAddrs...)								|					v
// 		g.peers = gee.RegisterPeers(httpPool)						|			g.getFromPeer(peerGetter, key)
// 						|											|					|
// 						|											|					|
// 						v											|					v
// 		http.ListenAndServe("localhost:800x", httpPool)<------------+--------------peerGetter.Get(key)
// 						|											|
// 						|requsets									|
// 						v											|
// 					p.ServeHttp(w, r)								|
// 						|											|
// 						|url.parse()								|
// 						|--------------------------------------------
