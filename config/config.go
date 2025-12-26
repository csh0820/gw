package config

import (
	"github.com/spf13/viper"
	"log"
	"sync"
)

// Config
// global config
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	ETCD      ETCDConfig      `mapstructure:"etcd"`
	Discovery DiscoveryConfig `mapstructure:"discovery"`
	Gateway   GatewayConfig   `mapstructure:"gateway"`
	// Route          []RouteConfig        `mapstructure:"route"`
	RateLimit      RateLimitConfig      `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	HTTPPool       HTTPPoolConfig       `mapstructure:"http_pool"`
	Logging        LoggingConfig        `mapstructure:"logging"`
	Metrics        MetricsConfig        `mapstructure:"metrics"`
}

type ServerConfig struct {
	Address      string `mapstructure:"address"`
	Mode         string `mapstructure:"mode"`
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
	IdleTimeout  int    `mapstructure:"idle_timeout"`
}

type ETCDConfig struct {
	Endpoints   []string `mapstructure:"endpoints"`
	DialTimeout int      `mapstructure:"dial_timeout"`
	Username    string   `mapstructure:"username"`
	Password    string   `mapstructure:"password"`
	Namespace   string   `mapstructure:"namespace"`
	LeaseTTL    int      `mapstructure:"lease_ttl"`
}

type DiscoveryConfig struct {
	Prefix          string `mapstructure:"prefix"`
	WatchInterval   int    `mapstructure:"watch_interval"`
	CacheExpiration int    `mapstructure:"cache_expiration"`
}

type GatewayConfig struct {
	DefaultTimeout int   `mapstructure:"default_timeout"`
	MaxBodySize    int64 `mapstructure:"max_body_size"`
	StripPrefix    bool  `mapstructure:"strip_prefix"`
}

// type RouteConfig struct {
// 	ServiceName string
// 	PathPrefix  string
// 	StripPrefix bool
// 	Timeout     time.Duration
// }

type RateLimitConfig struct {
	Enabled bool `mapstructure:"enabled"`
	RPS     int  `mapstructure:"rps"`
	Burst   int  `mapstructure:"burst"`
}

type CircuitBreakerConfig struct {
	Enabled             bool `mapstructure:"enabled"`
	FailureThreshold    int  `mapstructure:"failure_threshold"`
	RecoveryTimeout     int  `mapstructure:"recovery_timeout"`
	HalfOpenMaxRequests int  `mapstructure:"half_open_max_requests"`
}

type HTTPPoolConfig struct {
	MaxIdleConns        int `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost int `mapstructure:"max_idle_conns_per_host"`
	MaxConnsPerHost     int `mapstructure:"max_conns_per_host"`
	IdleConnTimeout     int `mapstructure:"idle_conn_timeout"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

var (
	once   sync.Once
	config *Config
)

func GetConfig() *Config {
	once.Do(func() {
		config = LoadConfig()
	})
	return config
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// 设置环境变量前缀
	viper.SetEnvPrefix("GATEWAY")
	viper.AutomaticEnv()

	// 设置默认值
	setDefaults()

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Printf("Config file not found, using defaults and environment variables")
		} else {
			log.Fatalf("Error reading config file: %v", err)
		}
	}

	// 从环境变量覆盖配置
	bindEnvVars()

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		log.Fatalf("Unable to decode config into struct: %v", err)
	}

	return &config
}

func setDefaults() {
	viper.SetDefault("server.address", ":8080")
	viper.SetDefault("server.mode", "debug")
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 30)
	viper.SetDefault("server.idle_timeout", 120)

	viper.SetDefault("etcd.endpoints", []string{"localhost:2379"})
	viper.SetDefault("etcd.dial_timeout", 5)
	viper.SetDefault("etcd.lease_ttl", 10)
	viper.SetDefault("etcd.namespace", "microservices")

	viper.SetDefault("discovery.prefix", "/services")
	viper.SetDefault("discovery.watch_interval", 10)
	viper.SetDefault("discovery.cache_expiration", 30)

	viper.SetDefault("http_pool.max_idle_conns", 100)
	viper.SetDefault("http_pool.max_idle_conns_per_host", 10)
	viper.SetDefault("http_pool.max_conns_per_host", 50)
	viper.SetDefault("http_pool.idle_conn_timeout", 90)

	viper.SetDefault("rate_limit.enabled", true)
	viper.SetDefault("rate_limit.rps", 100)
	viper.SetDefault("rate_limit.burst", 50)
}

func bindEnvVars() {
	viper.BindEnv("server.address", "GATEWAY_SERVER_ADDRESS")
	viper.BindEnv("etcd.endpoints", "GATEWAY_ETCD_ENDPOINTS")
	viper.BindEnv("etcd.username", "GATEWAY_ETCD_USERNAME")
	viper.BindEnv("etcd.password", "GATEWAY_ETCD_PASSWORD")
	viper.BindEnv("rate_limit.rps", "GATEWAY_RATE_LIMIT_RPS")
}

// // WatchConfig 监听配置文件变化
// func WatchConfig(callback func(*Config)) {
// 	viper.WatchConfig()
// 	viper.OnConfigChange(func(e fsnotify.Event) {
// 		log.Printf("Config file changed: %s", e.Name)
// 		var newConfig Config
// 		if err := viper.Unmarshal(&newConfig); err == nil {
// 			callback(&newConfig)
// 		}
// 	})
// }
