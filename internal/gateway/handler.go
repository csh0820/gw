package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/csh0820/gw/config"
	"github.com/csh0820/gw/internal/discovery"
	"github.com/csh0820/gw/internal/loadbalancer"
	"github.com/csh0820/gw/internal/pool"
)

type GatewayHandler struct {
	registry        discovery.Registry
	httpPool        *pool.HTTPPool
	config          *config.Config
	logger          *zap.Logger
	lb              loadbalancer.LoadBalancer
	routes          map[string]*Route
	mu              sync.RWMutex
	proxies         map[string]*httputil.ReverseProxy
	circuitBreakers map[string]*CircuitBreaker
}

type Route struct {
	ServiceName string
	PathPrefix  string
	StripPrefix bool
	Timeout     time.Duration
	Instances   []*discovery.ServiceInstance
}

type CircuitBreaker struct {
	failures         int
	successes        int
	state            string // closed, open, half-open
	lastFailure      time.Time
	halfOpenAttempts int
	mu               sync.RWMutex
}

func NewHandler(
	registry discovery.Registry,
	httpPool *pool.HTTPPool,
	config *config.Config,
	logger *zap.Logger,
) *GatewayHandler {
	return &GatewayHandler{
		registry:        registry,
		httpPool:        httpPool,
		config:          config,
		logger:          logger,
		lb:              loadbalancer.NewRoundRobin(),
		routes:          make(map[string]*Route),
		proxies:         make(map[string]*httputil.ReverseProxy),
		circuitBreakers: make(map[string]*CircuitBreaker),
	}
}

func (h *GatewayHandler) RegisterRoutes(router *gin.Engine) {
	// 动态路由处理
	router.Any("/*path", h.HandleRequest)

	// // 静态路由配置
	// for _, routeConfig := range h.config.Route {
	// 	h.routes[routeConfig.ServiceName] = &Route{
	// 		ServiceName: routeConfig.ServiceName,
	// 		PathPrefix:  routeConfig.PathPrefix,
	// 		StripPrefix: routeConfig.StripPrefix,
	// 		Timeout:     routeConfig.Timeout * time.Second,
	// 	}
	// }
}

func (h *GatewayHandler) HandleRequest(c *gin.Context) {
	path := c.Request.URL.Path

	// 查找匹配的路由
	route := h.findRoute(path)
	if route == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Service not found",
			"path":  path,
		})
		return
	}

	// 负载均衡选择实例
	instance := h.selectInstance(route)
	if instance == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "No available instances",
			"service": route.ServiceName,
		})
		return
	}

	// 检查熔断器
	if h.isCircuitOpen(instance.ID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":    "Circuit breaker open",
			"service":  route.ServiceName,
			"instance": instance.ID,
		})
		return
	}

	// 创建或获取反向代理
	proxy := h.getOrCreateProxy(instance, route)

	// 修改请求
	h.prepareRequest(c, route, instance)

	// 记录开始时间
	start := time.Now()

	// 执行代理请求
	proxy.ServeHTTP(c.Writer, c.Request)

	// 记录结果
	duration := time.Since(start)

	// 更新熔断器状态
	if c.Writer.Status() >= 500 {
		h.recordFailure(instance.ID)
		h.logger.Warn("Request failed",
			zap.String("service", route.ServiceName),
			zap.String("instance", instance.ID),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", duration),
		)
	} else {
		h.recordSuccess(instance.ID)
		h.logger.Debug("Request succeeded",
			zap.String("service", route.ServiceName),
			zap.String("instance", instance.ID),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", duration),
		)
	}
}

func (h *GatewayHandler) findRoute(path string) *Route {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 首先尝试前缀匹配
	for _, route := range h.routes {
		if route.PathPrefix != "" && strings.HasPrefix(path, route.PathPrefix) {
			return route
		}
	}

	// 尝试路径解析: /service-name/...
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		serviceName := parts[0]
		if route, exists := h.routes[serviceName]; exists {
			return route
		}

		// 动态路由
		return &Route{
			ServiceName: serviceName,
			PathPrefix:  "/" + serviceName,
			StripPrefix: h.config.Gateway.StripPrefix,
			Timeout:     time.Duration(h.config.Gateway.DefaultTimeout) * time.Second,
		}
	}

	return nil
}

func (h *GatewayHandler) selectInstance(route *Route) *discovery.ServiceInstance {
	h.mu.RLock()
	routeCopy := *route
	h.mu.RUnlock()

	// 获取最新的实例
	instances, err := h.registry.Discover(routeCopy.ServiceName)
	if err != nil {
		h.logger.Error("Failed to discover instances",
			zap.String("service", routeCopy.ServiceName),
			zap.Error(err),
		)
		return nil
	}

	// 过滤健康的实例
	healthyInstances := make([]*discovery.ServiceInstance, 0)
	for _, instance := range instances {
		if instance.Status == "UP" {
			healthyInstances = append(healthyInstances, instance)
		}
	}

	if len(healthyInstances) == 0 {
		return nil
	}

	// 使用负载均衡算法选择实例
	return h.lb.Select(healthyInstances)
}

func (h *GatewayHandler) getOrCreateProxy(instance *discovery.ServiceInstance, route *Route) *httputil.ReverseProxy {
	key := fmt.Sprintf("%s-%s", instance.Name, instance.ID)

	h.mu.RLock()
	proxy, exists := h.proxies[key]
	h.mu.RUnlock()

	if exists {
		return proxy
	}

	// 创建新的反向代理
	targetURL := fmt.Sprintf("http://%s:%d", instance.Address, instance.Port)
	target, err := url.Parse(targetURL)
	if err != nil {
		h.logger.Error("Failed to parse target URL",
			zap.String("url", targetURL),
			zap.Error(err),
		)
		return nil
	}

	proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			// 设置超时
			if route.Timeout > 0 {
				ctx, cancel := context.WithTimeout(req.Context(), route.Timeout)
				defer cancel()
				*req = *req.WithContext(ctx)
			}

			// 添加请求头
			req.Header.Set("X-Forwarded-For", req.RemoteAddr)
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Forwarded-Proto", req.URL.Scheme)
			req.Header.Set("X-Gateway", "gin-etcd-gateway")
		},
		Transport: h.httpPool.GetClient().Transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.logger.Error("Proxy error",
				zap.String("service", instance.Name),
				zap.String("instance", instance.ID),
				zap.Error(err),
			)
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	h.mu.Lock()
	h.proxies[key] = proxy
	h.mu.Unlock()

	return proxy
}

func (h *GatewayHandler) prepareRequest(c *gin.Context, route *Route, instance *discovery.ServiceInstance) {
	// 移除路径前缀
	if route.StripPrefix && route.PathPrefix != "" && route.PathPrefix != "/" {
		c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, route.PathPrefix)
		if c.Request.URL.Path == "" {
			c.Request.URL.Path = "/"
		}
	}

	// 添加追踪头
	c.Request.Header.Set("X-Service-Instance", instance.ID)
	c.Request.Header.Set("X-Service-Name", instance.Name)
}

func (h *GatewayHandler) StartServiceDiscovery(ctx context.Context) {
	// 监听所有服务变化
	ch, err := h.registry.WatchAll()
	if err != nil {
		h.logger.Error("Failed to watch services", zap.Error(err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case services := <-ch:
			h.updateRoutes(services)
		}
	}
}

func (h *GatewayHandler) updateRoutes(services map[string][]*discovery.ServiceInstance) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for serviceName, instances := range services {
		if route, exists := h.routes[serviceName]; exists {
			route.Instances = instances
		} else {
			// 动态创建路由
			h.routes[serviceName] = &Route{
				ServiceName: serviceName,
				PathPrefix:  "/" + serviceName,
				StripPrefix: h.config.Gateway.StripPrefix,
				Timeout:     time.Duration(h.config.Gateway.DefaultTimeout) * time.Second,
				Instances:   instances,
			}
		}

		h.logger.Info("Service route updated",
			zap.String("service", serviceName),
			zap.Int("instances", len(instances)),
		)
	}
}

func (h *GatewayHandler) ListServices(c *gin.Context) {
	services, err := h.registry.GetAllServices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	result := make(map[string]interface{})
	for serviceName, instances := range services {
		healthy := 0
		for _, instance := range instances {
			if instance.Status == "UP" {
				healthy++
			}
		}

		result[serviceName] = gin.H{
			"total_instances":   len(instances),
			"healthy_instances": healthy,
			"instances":         instances,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"services":  result,
		"timestamp": time.Now().Unix(),
	})
}

func (h *GatewayHandler) RefreshServices(c *gin.Context) {
	// 手动刷新服务
	services, err := h.registry.GetAllServices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	h.updateRoutes(services)

	c.JSON(http.StatusOK, gin.H{
		"message":  "Services refreshed",
		"services": len(services),
	})
}

// 熔断器相关方法
func (h *GatewayHandler) isCircuitOpen(instanceID string) bool {
	if !h.config.CircuitBreaker.Enabled {
		return false
	}

	h.mu.RLock()
	cb, exists := h.circuitBreakers[instanceID]
	h.mu.RUnlock()

	if !exists {
		return false
	}

	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == "open" {
		// 检查是否应该进入半开状态
		if time.Since(cb.lastFailure) > time.Duration(h.config.CircuitBreaker.RecoveryTimeout)*time.Second {
			cb.state = "half-open"
			cb.halfOpenAttempts = 0
		}
		return true
	}

	return false
}

func (h *GatewayHandler) recordFailure(instanceID string) {
	if !h.config.CircuitBreaker.Enabled {
		return
	}

	h.mu.Lock()
	cb, exists := h.circuitBreakers[instanceID]
	if !exists {
		cb = &CircuitBreaker{
			state: "closed",
		}
		h.circuitBreakers[instanceID] = cb
	}
	h.mu.Unlock()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == "half-open" {
		// 半开状态失败，立即转回开启
		cb.state = "open"
		cb.lastFailure = time.Now()
		cb.halfOpenAttempts = 0
		return
	}

	cb.failures++
	cb.successes = 0

	if cb.failures >= h.config.CircuitBreaker.FailureThreshold {
		cb.state = "open"
		cb.lastFailure = time.Now()
	}
}

func (h *GatewayHandler) recordSuccess(instanceID string) {
	if !h.config.CircuitBreaker.Enabled {
		return
	}

	h.mu.Lock()
	cb, exists := h.circuitBreakers[instanceID]
	if !exists {
		return
	}
	h.mu.Unlock()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == "half-open" {
		cb.halfOpenAttempts++
		if cb.halfOpenAttempts >= h.config.CircuitBreaker.HalfOpenMaxRequests {
			// 成功次数达到阈值，关闭熔断器
			cb.state = "closed"
			cb.failures = 0
			cb.successes = 0
			cb.halfOpenAttempts = 0
		}
	} else {
		cb.successes++
		if cb.successes > 0 {
			cb.failures = 0
		}
	}
}
