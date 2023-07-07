package singleflight

import "sync"

// call 代表正在进行中，或已经结束的请求
type call struct {
	wg  sync.WaitGroup // 使用 sync.WaitGroup 锁避免重入
	val interface{}
	err error
}

// Set 管理不同 key 的请求(call)
type Set struct {
	mu sync.Mutex // 保护成员变量 mp 不被并发【读写】而加上的锁
	mp map[string]*call
}

// Do 保证并发时同一请求只请求一次，返回相同结果
func (s *Set) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	s.mu.Lock() // 防止并发读和写
	if s.mp == nil {
		s.mp = make(map[string]*call)
	}
	if c, ok := s.mp[key]; ok {
		s.mu.Unlock()       // 读完毕，可以解锁
		c.wg.Wait()         // 如果请求正在进行中，则等待
		return c.val, c.err // 请求结束，返回结果
	}
	c := new(call)
	c.wg.Add(1)   // 发起请求前加锁
	s.mp[key] = c // 添加到 s.mp，表明 key 已经有对应的请求在处理
	s.mu.Unlock() // 写完毕，可以解锁

	c.val, c.err = fn() // 调用 fn，发起请求
	c.wg.Done()         // 请求结束

	s.mu.Lock()
	delete(s.mp, key) // 本方法只是为了同一时刻相同并发请求而设计，所以本次请求完成应删除key，以便下次更新 s.mp
	s.mu.Unlock()

	return c.val, c.err // 返回结果
}
