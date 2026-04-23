package utils

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTLCache_PutAndGet(t *testing.T) {
	cache := NewTTLCache[string, int](time.Minute)
	cache.Put("a", 1)
	cache.Put("b", 2)

	v, ok := cache.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)

	v, ok = cache.Get("b")
	require.True(t, ok)
	assert.Equal(t, 2, v)
}

func TestTTLCache_Miss(t *testing.T) {
	cache := NewTTLCache[string, int](time.Minute)

	v, ok := cache.Get("missing")
	assert.False(t, ok)
	assert.Equal(t, 0, v)
}

func TestTTLCache_Expiry(t *testing.T) {
	cache := NewTTLCache[string, string](50 * time.Millisecond)
	cache.Put("key", "value")

	v, ok := cache.Get("key")
	require.True(t, ok)
	assert.Equal(t, "value", v)

	time.Sleep(60 * time.Millisecond)

	v, ok = cache.Get("key")
	assert.False(t, ok, "should be expired")
	assert.Equal(t, "", v)
}

func TestTTLCache_Overwrite(t *testing.T) {
	cache := NewTTLCache[string, int](time.Minute)
	cache.Put("k", 1)
	cache.Put("k", 2)

	v, ok := cache.Get("k")
	require.True(t, ok)
	assert.Equal(t, 2, v)
}

func TestTTLCache_Clear(t *testing.T) {
	cache := NewTTLCache[string, int](time.Minute)
	cache.Put("a", 1)
	cache.Put("b", 2)

	cache.Clear()

	_, ok := cache.Get("a")
	assert.False(t, ok)
	_, ok = cache.Get("b")
	assert.False(t, ok)
	assert.Equal(t, 0, cache.Len())
}

func TestTTLCache_Len_ExcludesExpired(t *testing.T) {
	cache := NewTTLCache[string, int](50 * time.Millisecond)
	cache.Put("expires", 1)

	longCache := NewTTLCache[string, int](time.Minute)
	longCache.Put("stays", 2)
	longCache.Put("also_stays", 3)

	assert.Equal(t, 1, cache.Len())
	assert.Equal(t, 2, longCache.Len())

	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, 0, cache.Len(), "expired entry should not be counted")
}

func TestTTLCache_ConcurrentAccess(t *testing.T) {
	cache := NewTTLCache[int, int](time.Minute)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cache.Put(n, n*10)
			cache.Get(n)
		}(i)
	}
	wg.Wait()

	count := 0
	for i := 0; i < 100; i++ {
		if v, ok := cache.Get(i); ok {
			assert.Equal(t, i*10, v)
			count++
		}
	}
	assert.Equal(t, 100, count)
}
