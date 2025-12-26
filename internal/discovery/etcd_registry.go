package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/csh0820/gw/pkg/etcd"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type ServiceInstance struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	Metadata   map[string]string `json:"metadata"`
	Tags       []string          `json:"tags"`
	Status     string            `json:"status"` // UP, DOWN, STARTING
	Registered time.Time         `json:"registered"`
	LastSeen   time.Time         `json:"last_seen"`
	Version    string            `json:"version"`
	Weight     int               `json:"weight"`
}

type Registry interface {
	Register(instance *ServiceInstance) error
	Deregister(instanceID string) error
	Discover(serviceName string) ([]*ServiceInstance, error)
	GetAllServices() (map[string][]*ServiceInstance, error)
	Watch(serviceName string) (<-chan []*ServiceInstance, error)
	WatchAll() (<-chan map[string][]*ServiceInstance, error)
}

type EtcdRegistry struct {
	client     *etcd.Client
	prefix     string
	logger     *zap.Logger
	cache      *ServiceCache
	mu         sync.RWMutex
	watchers   map[string][]chan []*ServiceInstance
	watchersMu sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewEtcdRegistry(client *etcd.Client, logger *zap.Logger) *EtcdRegistry {
	ctx, cancel := context.WithCancel(context.Background())

	registry := &EtcdRegistry{
		client:   client,
		prefix:   viper.GetString("discovery.prefix"),
		logger:   logger,
		cache:    NewServiceCache(time.Duration(viper.GetInt("discovery.cache_expiration")) * time.Second),
		watchers: make(map[string][]chan []*ServiceInstance),
		ctx:      ctx,
		cancel:   cancel,
	}

	// 启动监听
	go registry.watchAllServices()

	return registry
}

func (r *EtcdRegistry) getServiceKey(serviceName string) string {
	return fmt.Sprintf("%s/%s", r.prefix, serviceName)
}

func (r *EtcdRegistry) getInstanceKey(serviceName, instanceID string) string {
	return fmt.Sprintf("%s/%s/instances/%s", r.prefix, serviceName, instanceID)
}

func (r *EtcdRegistry) Register(instance *ServiceInstance) error {
	instance.Registered = time.Now()
	instance.LastSeen = time.Now()
	instance.Status = "UP"

	if instance.ID == "" {
		instance.ID = fmt.Sprintf("%s-%s-%d", instance.Name, instance.Address, instance.Port)
	}

	// 序列化实例数据
	data, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to marshal instance: %w", err)
	}

	key := r.getInstanceKey(instance.Name, instance.ID)

	// 使用租约注册，自动过期
	ttl := viper.GetInt64("etcd.lease_ttl")
	leaseID, err := r.client.PutWithLease(r.ctx, key, string(data), ttl)
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	r.logger.Info("Service registered",
		zap.String("service", instance.Name),
		zap.String("instance", instance.ID),
		zap.String("address", fmt.Sprintf("%s:%d", instance.Address, instance.Port)),
		zap.String("lease", leaseID),
	)

	return nil
}

func (r *EtcdRegistry) Deregister(instanceID string) error {
	// 从所有服务中查找并删除
	services, err := r.GetAllServices()
	if err != nil {
		return err
	}

	for serviceName, instances := range services {
		for _, instance := range instances {
			if instance.ID == instanceID {
				key := r.getInstanceKey(serviceName, instanceID)
				_, err := r.client.Delete(r.ctx, key)
				if err != nil {
					return fmt.Errorf("failed to deregister service: %w", err)
				}

				r.logger.Info("Service deregistered",
					zap.String("service", serviceName),
					zap.String("instance", instanceID),
				)

				// 清除缓存
				r.cache.Delete(serviceName)

				// 通知监听器
				r.notifyWatchers(serviceName)

				return nil
			}
		}
	}

	return fmt.Errorf("instance %s not found", instanceID)
}

func (r *EtcdRegistry) Discover(serviceName string) ([]*ServiceInstance, error) {
	// 检查缓存
	if instances, found := r.cache.Get(serviceName); found {
		return instances, nil
	}

	// 从ETCD查询
	prefix := r.getServiceKey(serviceName)
	instances, err := r.getInstancesFromETCD(prefix)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	r.cache.Set(serviceName, instances)

	return instances, nil
}

func (r *EtcdRegistry) getInstancesFromETCD(prefix string) ([]*ServiceInstance, error) {
	kvPairs, err := r.client.GetWithPrefix(r.ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances from etcd: %w", err)
	}

	instances := make([]*ServiceInstance, 0)

	for key, value := range kvPairs {
		if strings.Contains(key, "/instances/") {
			var instance ServiceInstance
			if err := json.Unmarshal([]byte(value), &instance); err != nil {
				r.logger.Error("Failed to unmarshal instance",
					zap.String("key", key),
					zap.Error(err),
				)
				continue
			}

			// 检查实例是否过期
			if time.Since(instance.LastSeen) > time.Duration(viper.GetInt("etcd.lease_ttl"))*time.Second {
				r.logger.Warn("Instance expired",
					zap.String("instance", instance.ID),
					zap.Duration("age", time.Since(instance.LastSeen)),
				)
				continue
			}

			instances = append(instances, &instance)
		}
	}

	return instances, nil
}

func (r *EtcdRegistry) GetAllServices() (map[string][]*ServiceInstance, error) {
	kvPairs, err := r.client.GetWithPrefix(r.ctx, r.prefix)
	if err != nil {
		return nil, err
	}

	services := make(map[string][]*ServiceInstance)

	for key, value := range kvPairs {
		if strings.Contains(key, "/instances/") {
			var instance ServiceInstance
			if err := json.Unmarshal([]byte(value), &instance); err != nil {
				continue
			}

			if _, exists := services[instance.Name]; !exists {
				services[instance.Name] = make([]*ServiceInstance, 0)
			}

			services[instance.Name] = append(services[instance.Name], &instance)
		}
	}

	return services, nil
}

func (r *EtcdRegistry) Watch(serviceName string) (<-chan []*ServiceInstance, error) {
	ch := make(chan []*ServiceInstance, 10)

	r.watchersMu.Lock()
	r.watchers[serviceName] = append(r.watchers[serviceName], ch)
	r.watchersMu.Unlock()

	return ch, nil
}

func (r *EtcdRegistry) WatchAll() (<-chan map[string][]*ServiceInstance, error) {
	// 实现全服务监听
	return nil, nil
}

func (r *EtcdRegistry) watchAllServices() {
	watchChan := r.client.WatchPrefix(r.ctx, r.prefix)

	for {
		select {
		case <-r.ctx.Done():
			return
		case resp := <-watchChan:
			if resp.Err() != nil {
				r.logger.Error("Watch error", zap.Error(resp.Err()))
				continue
			}

			for _, event := range resp.Events {
				key := string(event.Kv.Key)

				// 提取服务名
				parts := strings.Split(strings.TrimPrefix(key, r.prefix+"/"), "/")
				if len(parts) >= 2 && parts[1] == "instances" {
					serviceName := parts[0]

					// 清除缓存
					r.cache.Delete(serviceName)

					// 通知监听器
					r.notifyWatchers(serviceName)
				}
			}
		}
	}
}

func (r *EtcdRegistry) notifyWatchers(serviceName string) {
	instances, err := r.Discover(serviceName)
	if err != nil {
		r.logger.Error("Failed to discover instances for watcher",
			zap.String("service", serviceName),
			zap.Error(err),
		)
		return
	}

	r.watchersMu.RLock()
	watchers, exists := r.watchers[serviceName]
	r.watchersMu.RUnlock()

	if exists {
		for _, watcher := range watchers {
			select {
			case watcher <- instances:
				// 成功发送
			default:
				r.logger.Warn("Watcher channel full",
					zap.String("service", serviceName),
				)
			}
		}
	}
}

func (r *EtcdRegistry) Close() {
	r.cancel()

	// 关闭所有监听器
	r.watchersMu.Lock()
	for _, watchers := range r.watchers {
		for _, ch := range watchers {
			close(ch)
		}
	}
	r.watchers = make(map[string][]chan []*ServiceInstance)
	r.watchersMu.Unlock()
}
