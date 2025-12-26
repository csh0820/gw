package discovery

import (
	"sync"
	"time"
)

type ServiceCache struct {
	cache    map[string]*cacheEntry
	mu       sync.RWMutex
	duration time.Duration
}

type cacheEntry struct {
	instances []*ServiceInstance
	timestamp time.Time
}

func NewServiceCache(duration time.Duration) *ServiceCache {
	return &ServiceCache{
		cache:    make(map[string]*cacheEntry),
		duration: duration,
	}
}

func (c *ServiceCache) Get(serviceName string) ([]*ServiceInstance, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.cache[serviceName]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Since(entry.timestamp) > c.duration {
		return nil, false
	}

	return entry.instances, true
}

func (c *ServiceCache) Set(serviceName string, instances []*ServiceInstance) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[serviceName] = &cacheEntry{
		instances: instances,
		timestamp: time.Now(),
	}
}

func (c *ServiceCache) Delete(serviceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, serviceName)
}

func (c *ServiceCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheEntry)
}
