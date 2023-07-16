package main

import (
	"fmt"
	"geecache"
	"log"
	"net/http"
	"strings"
	"sync"
)

var db2 = map[string]string{
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

func main() {
	peers := []string{
		"http://localhost:8001",
		"http://localhost:8002",
		"http://localhost:8003",
		"http://localhost:8004",
		"http://localhost:8005",
		"http://localhost:8006",
	}
	gp := geecache.NewGroup("scores", 2<<10, geecache.GetterFunc(
		func(key string) ([]byte, error) {
			log.Println("[slowDB] search key: " + key)
			if v, ok := db[key]; ok {
				return []byte(v), nil
			}
			return nil, fmt.Errorf("%s not exist", key)
		}))

	htpPol := geecache.NewHTTPPool(peers[0], "", 0, nil)
	htpPol.SetPeers(peers...)

	var wg sync.WaitGroup
	wg.Add(len(peers))
	for _, peer := range peers {
		go func(p string) {
			http.Handle("/_geecache", http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					parts := strings.SplitN(r.URL.Path[11:], "/", -1)
					if len(parts) != 2 {
						http.Error(w, "bad request", http.StatusBadRequest)
						return
					}

					view, err := gp.Query(parts[1])
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					w.Header().Set("Content-Type", "application/octet-stream")
					w.Write(view.ByteSlice())
				}))
			log.Println("geecache is running at", p)
			err := http.ListenAndServe(p[7:], nil)
			if err != nil {
				log.Fatal(err)
			}
			wg.Done()
		}(peer)
	}
	wg.Wait()
}
