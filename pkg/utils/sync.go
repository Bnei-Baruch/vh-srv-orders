package utils

import "sync"

type Number interface {
	~int64 | ~float64
}

type CounterMap[T Number] struct {
	mu sync.Mutex
	m  map[string]T
}

func NewCounterMap[T Number]() *CounterMap[T] {
	return &CounterMap[T]{m: make(map[string]T)}
}

func (c *CounterMap[T]) Inc(key string, delta T) {
	c.mu.Lock()
	c.m[key] += delta
	c.mu.Unlock()
}

func (c *CounterMap[T]) Get(key string) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[key]
}
