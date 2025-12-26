# Golang 微服务网关

一个基于 Gin 框架的微服务网关，实现了动态服务发现和反向代理功能。

## 功能特性

- ✅ 使用 Gin 框架构建高性能网关
- ✅ 基于 Etcd 的动态服务发现
- ✅ 支持 HTTP 连接池配置
- ✅ 使用 Viper 进行配置管理

## 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置文件

编辑 `config.yaml` 文件，配置网关和 Etcd 信息：

```yaml

```

### 3. 启动 Etcd

确保 Etcd 服务已经启动：

```bash
etcd
```

### 4. 启动网关

```bash
go run main.go
```

### 5. 注册服务到 Etcd

使用 Etcd 客户端注册服务：

```bash
etcdctl put /services/user-service 127.0.0.1:8081
```

### 6. 访问服务

通过网关访问注册的服务：

```bash
curl http://localhost:8080/user-service/path/to/resource
```

## 架构设计

### 1. 服务发现流程

1. 服务启动时将自己的地址注册到 Etcd
2. 网关从 Etcd 获取服务地址列表
3. 网关监控 Etcd 中服务地址的变化
4. 当服务地址变化时，网关自动更新本地缓存

### 2. 反向代理流程

1. 客户端向网关发送请求
2. 网关从请求路径提取服务名称
3. 网关从服务发现获取服务地址
4. 网关使用连接池向服务发送请求
5. 网关将服务响应返回给客户端

## 配置说明

### 网关配置

| 配置项 | 类型 | 描述 | 默认值 |
|-------|------|------|-------|
| gateway.port | string | 网关监听端口 | 8080 |
| gateway.read_timeout | duration | 请求读取超时 | 5s |
| gateway.write_timeout | duration | 响应写入超时 | 5s |
| gateway.idle_timeout | duration | 连接空闲超时 | 60s |

### Etcd 配置

| 配置项 | 类型 | 描述 | 默认值 |
|-------|------|------|-------|
| etcd.endpoints | []string | Etcd 服务地址列表 | ["localhost:2379"] |
| etcd.dial_timeout | duration | Etcd 连接超时 | 5s |
| etcd.username | string | Etcd 用户名 | "" |
| etcd.password | string | Etcd 密码 | "" |

### HTTP 客户端配置

| 配置项 | 类型 | 描述 | 默认值 |
|-------|------|------|-------|
| http_client.max_idle_conns | int | 最大空闲连接数 | 100 |
| http_client.max_idle_conns_per_host | int | 每个主机的最大空闲连接数 | 10 |
| http_client.idle_conn_timeout | duration | 空闲连接超时时间 | 90s |
| http_client.timeout | duration | 请求超时时间 | 10s |

## 扩展建议

1. **负载均衡**：目前网关返回第一个可用服务，可以扩展实现轮询、随机、加权轮询等负载均衡算法
2. **服务健康检查**：添加服务健康检查机制，自动过滤不健康的服务实例
3. **限流与熔断**：实现限流和熔断机制，保护后端服务
4. **认证与授权**：添加认证和授权功能，控制访问权限
5. **监控与日志**：增强监控和日志功能，方便问题排查

## 许可证

MIT
