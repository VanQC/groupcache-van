package singleflight

import (
	"errors"
	"fmt"
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

func TestDo(t *testing.T) {
	var g Set
	v, err := g.Do("key", func() (interface{}, error) {
		return "bar", nil
	})
	if got, want := fmt.Sprintf("%v (%T)", v, v), "bar (string)"; got != want {
		t.Errorf("Do = %v; want %v", got, want)
	}
	if err != nil {
		t.Errorf("Do error = %v", err)
	}
}

func TestDoErr(t *testing.T) {
	var g Set
	someErr := errors.New("Some error")
	v, err := g.Do("key", func() (interface{}, error) {
		return nil, someErr
	})
	if err != someErr {
		t.Errorf("Do error = %v; want someErr", err)
	}
	if v != nil {
		t.Errorf("unexpected non-nil value %#v", v)
	}
}
