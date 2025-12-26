package pool

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

type Config struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration
	DisableCompression  bool
	TLSClientConfig     *tls.Config
}

type HTTPPool struct {
	client *http.Client
	config *Config
}

func NewHTTPPool(config *Config) *HTTPPool {
	if config == nil {
		config = &Config{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			MaxConnsPerHost:     50,
			IdleConnTimeout:     90 * time.Second,
		}
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    config.DisableCompression,
		TLSClientConfig:       config.TLSClientConfig,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &HTTPPool{
		client: client,
		config: config,
	}
}

func (p *HTTPPool) GetClient() *http.Client {
	return p.client
}

func (p *HTTPPool) Do(req *http.Request) (*http.Response, error) {
	return p.client.Do(req)
}

func (p *HTTPPool) CloseIdleConnections() {
	if p.client.Transport != nil {
		if transport, ok := p.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

func (p *HTTPPool) UpdateConfig(config *Config) {
	p.config = config

	// 重新创建Transport
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    config.DisableCompression,
		TLSClientConfig:       config.TLSClientConfig,
	}

	p.client.Transport = transport
}

func (p *HTTPPool) GetMetrics() map[string]interface{} {
	if transport, ok := p.client.Transport.(*http.Transport); ok {
		return map[string]interface{}{
			"max_idle_conns":          transport.MaxIdleConns,
			"max_idle_conns_per_host": transport.MaxIdleConnsPerHost,
			"max_conns_per_host":      transport.MaxConnsPerHost,
			"idle_conn_timeout":       transport.IdleConnTimeout.String(),
		}
	}
	return nil
}
