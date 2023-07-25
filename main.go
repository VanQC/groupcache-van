package main

import (
	"flag"
	"fmt"
	"geecache"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"sync"
	"time"
)

var db = map[string]string{
	"Tom":   "630",
	"Jack":  "589",
	"Sam":   "567",
	"John":  "678",
	"Amy":   "532",
	"David": "612",
	"Lisa":  "658",
	"Eric":  "698",
	"Kate":  "620",
	"Mike":  "672",
	"Sara":  "655",
	"Ben":   "695",
	"Alex":  "675",
}

type entry struct {
	Key   string `json:"key"`
	Value string
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

func startAPIServer(apiAddr string, gp *geecache.Group) {
	r := gin.Default()

	r.GET("/get", func(ctx *gin.Context) {
		key := ctx.Query("key") //获取请求携带的参数数据
		view, err := gp.Query(key)
		if err != nil {
			ctx.String(http.StatusInternalServerError, err.Error())
			return
		}
		ctx.Writer.Write(view.ByteSlice())
	})
	r.GET("/remove", func(ctx *gin.Context) {
		key := ctx.Query("key") //获取请求携带的参数数据
		err := gp.Remove(key)
		if err != nil {
			ctx.String(http.StatusInternalServerError, err.Error())
			return
		}
		ctx.Writer.Write([]byte("remove key success"))
	})

	r.POST("/set", func(c *gin.Context) {
		var res entry
		if err := c.ShouldBind(&res); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("key:%v type, value:%v", res.Key, res.Value)
		err := gp.Set(res.Key, []byte(res.Value), time.Time{}, true)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Writer.Write([]byte("set key success"))
	})

	log.Println("fontend server is running at", apiAddr)
	r.Run(apiAddr[7:])
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

	addrMap := map[int]string{
		8001: "127.0.0.1:8001",
		8002: "127.0.0.1:8002",
		8003: "127.0.0.1:8003",
	}
	var addrs []string
	for _, v := range addrMap {
		addrs = append(addrs, v)
	}
	group := creatGroup()
	s := geecache.NewServer(addrMap[port], 0, nil)
	s.SetPeers(addrs...)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		// Start将不会return 除非服务stop或者抛出error
		err := s.Start()
		if err != nil {
			log.Fatal(err)
		}
		wg.Done()
	}()

	apiAddr := "http://localhost:9999"
	if api {
		go startAPIServer(apiAddr, group)
	}

	wg.Wait()
}
