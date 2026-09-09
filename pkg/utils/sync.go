package utils

import (
	"sync"
	"time"
)

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

// TTLCache is a generic thread-safe cache with per-entry expiration.
// Expired entries are lazily evicted on access.
type TTLCache[K comparable, V any] struct {
	mu  sync.RWMutex
	m   map[K]ttlEntry[V]
	ttl time.Duration
}

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

func NewTTLCache[K comparable, V any](ttl time.Duration) *TTLCache[K, V] {
	return &TTLCache[K, V]{m: make(map[K]ttlEntry[V]), ttl: ttl}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	entry, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		var zero V
		return zero, false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.m, key)
		c.mu.Unlock()
		var zero V
		return zero, false
	}
	return entry.value, true
}

func (c *TTLCache[K, V]) Put(key K, value V) {
	c.mu.Lock()
	c.m[key] = ttlEntry[V]{value: value, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

func (c *TTLCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, entry := range c.m {
		if now.Before(entry.expiresAt) {
			count++
		}
	}
	return count
}

func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	c.m = make(map[K]ttlEntry[V])
	c.mu.Unlock()
}
