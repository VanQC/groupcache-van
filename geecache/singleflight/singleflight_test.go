package singleflight

import (
	"sync"
	"testing"
	"time"
)

func TestDoDuplicate(t *testing.T) {
	var (
		s   Set
		num = 0
		wg  sync.WaitGroup
	)
	c := make(chan string)
	fn := func() (any, error) {
		num++
		return <-c, nil
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			v, err := s.Do("key", fn)
			if err != nil {
				t.Errorf("Do error: %v", err)
			}
			if v.(string) != "bar" {
				t.Errorf("got %q; want %q", v, "bar")
			}
			wg.Done()
		}()
	}
	time.Sleep(100 * time.Millisecond) // let goroutines above block
	c <- "bar"
	wg.Wait()
	if num != 1 {
		t.Errorf("number of calls = %d; want 1", num)
	}
}
