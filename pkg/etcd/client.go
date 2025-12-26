package etcd

import (
	"context"
	"fmt"
	"github.com/csh0820/gw/config"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

type Client struct {
	*clientv3.Client
	namespace string
	logger    *zap.Logger
}

func NewClientFromConfig(logger *zap.Logger) (*Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: config.GetConfig().ETCD.Endpoints,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cli.Status(ctx, config.GetConfig().ETCD.Endpoints[0])
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	return &Client{
		Client: cli,
		// namespace: cfg.Namespace,
		logger: logger,
	}, nil
}

func (c *Client) GetKey(key string) string {
	if c.namespace == "" {
		return key
	}

	return fmt.Sprintf("%s/%s", c.namespace, strings.TrimPrefix(key, "/"))
}

func (c *Client) PutWithLease(ctx context.Context, key, value string, ttl int64) (string, error) {
	key = c.GetKey(key)

	// 创建租约
	resp, err := c.Grant(ctx, ttl)
	if err != nil {
		return "", fmt.Errorf("failed to create lease: %w", err)
	}

	// 使用租约写入数据
	_, err = c.Put(ctx, key, value, clientv3.WithLease(resp.ID))
	if err != nil {
		return "", fmt.Errorf("failed to put key with lease: %w", err)
	}

	// 保持租约活跃
	keepAliveChan, err := c.KeepAlive(ctx, resp.ID)
	if err != nil {
		return "", fmt.Errorf("failed to keep lease alive: %w", err)
	}

	// 消费keepAlive响应，防止chan阻塞
	go func() {
		for range keepAliveChan {
			// 保持租约活跃
		}
	}()

	return fmt.Sprintf("%d", resp.ID), nil
}

func (c *Client) WatchPrefix(ctx context.Context, prefix string) clientv3.WatchChan {
	key := c.GetKey(prefix)
	return c.Watch(ctx, key, clientv3.WithPrefix())
}

func (c *Client) GetWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	key := c.GetKey(prefix)
	resp, err := c.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, kv := range resp.Kvs {
		result[string(kv.Key)] = string(kv.Value)
	}

	return result, nil
}

func (c *Client) Close() error {
	if c.Client != nil {
		return c.Client.Close()
	}
	return nil
}
