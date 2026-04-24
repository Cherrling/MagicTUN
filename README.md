# MagicTun

多层内网代理工具，使用 Go 编写。多个节点部署在不同层级的内网中，节点间自动发现并建立路由，每个节点暴露本地 SOCKS5 端口供应用使用。

**只需配置 bootstrap 节点，其余全部自动完成。**

## 核心原理

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Node A    │◄───►│   Node B    │◄───►│   Node C    │
│  (出口节点)  │     │  (中间节点)  │     │  (入口节点)  │
│             │     │             │     │             │
│ SOCKS5:1080 │     │ SOCKS5:1080 │     │ SOCKS5:1080 │
│ 直连:外网    │     │ 直连:10.2/16│     │ 直连:10.3/16│
└─────────────┘     └─────────────┘     └─────────────┘
       ▲                                       ▲
       │ 自动路由: 10.3.0.0/16 → B → C         │
       └───────────────────────────────────────┘
```

- **路由算法**：Path-Vector（类 BGP），AS-Path 防环，Cost 选路
- **节点发现**：Gossip/SWIM 协议，自动发现并收敛 peer 列表
- **传输协议**：TCP+TLS + 自定义帧多路复用，单连接承载控制流和数据流
- **身份认证**：Ed25519 密钥对
- **客户端接口**：SOCKS5（CONNECT）

## 快速开始

### 编译

```bash
make build
# 或者
go build -o bin/magictun ./cmd/magictun/
```

### 使用

**场景：两台机器，A 能访问外网，B 需要通过 A 代理上网。**

在 A（出口节点）上：

```bash
./bin/magictun -name exit -listen :9443 -socks :1080 -net 0.0.0.0/0
```

在 B（入口节点）上：

```bash
./bin/magictun -name entry -listen :9444 -socks :1081 -peers A的IP:9443
```

然后在 B 上使用代理：

```bash
curl --socks5 127.0.0.1:1081 https://www.example.com
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-name` | `magictun` | 节点名称 |
| `-listen` | `:9443` | 节点间通信监听地址 |
| `-socks` | `:1080` | 本地 SOCKS5 代理监听地址 |
| `-peers` | (无) | bootstrap 节点地址，逗号分隔，格式 `host:port` |
| `-net` | (无) | 本节点直连的网段，逗号分隔，格式 `CIDR` |
| `-identity` | `/tmp/magictun-<name>.key` | Ed25519 密钥文件路径 |
| `-config` | (无) | JSON 配置文件路径（可选，指定后忽略其他命令行参数） |

### 三层拓扑示例

```
内层 (Node C)  →  中层 (Node B)  →  外层 (Node A)  →  互联网
  socks5 :1082       socks5 :1081       socks5 :1080
  -peers B:9444      -peers A:9443      -net 0.0.0.0/0
  -net 10.3.0.0/24   -net 10.2.0.0/24
```

Node A：
```bash
./bin/magictun -name a -listen :9443 -socks :1080 -net 0.0.0.0/0
```

Node B：
```bash
./bin/magictun -name b -listen :9444 -socks :1081 -peers A的IP:9443 -net 10.2.0.0/24
```

Node C：
```bash
./bin/magictun -name c -listen :9445 -socks :1082 -peers B的IP:9444 -net 10.3.0.0/24
```

C 的 SOCKS5 流量自动经过 B→A 到达外网，无需手动配置任何路由。

### 配置文件方式

也可以用 JSON 配置文件：

```bash
./bin/magictun -config config.json
```

```json
{
  "node": {
    "name": "exit-node",
    "identity_file": "/etc/magictun/identity.key",
    "listen_addr": "0.0.0.0:9443",
    "socks5_addr": "127.0.0.1:1080"
  },
  "routing": {
    "direct_networks": ["0.0.0.0/0"],
    "route_advertisement_interval": "30s"
  },
  "gossip": {
    "bootstrap_peers": [],
    "push_interval": "5s",
    "peer_timeout": "15s"
  }
}
```

## 项目结构

```
magic_tun/
├── cmd/magictun/main.go       # 入口
├── config/config.go           # 配置解析
├── identity/                  # Ed25519 密钥与 NodeID
├── transport/                 # TCP+TLS 多路复用传输层
│   ├── conn.go                #   多路复用连接（帧协议）
│   ├── frame.go               #   帧编解码
│   ├── listener.go            #   TLS 监听器
│   ├── tls.go                 #   自签名 TLS 证书
│   └── udp.go                 #   UDP relay 套接字
├── wire/                      # 二进制线格式编解码
│   ├── header.go              #   消息类型常量
│   ├── route.go               #   路由通告
│   ├── gossip.go              #   Gossip 消息
│   ├── forward_tcp.go         #   TCP 转发头
│   └── forward_udp.go         #   UDP 转发头
├── gossip/                    # 节点发现
│   ├── peer.go                #   Peer 状态机
│   └── gossip.go              #   SWIM 引擎
├── route/                     # 路由
│   ├── path.go                #   AS-Path 防环
│   ├── table.go               #   Path-Vector 路由表
│   └── propagation.go         #   路由传播引擎
├── forward/                   # 数据转发
│   ├── tcp_relay.go           #   TCP 逐跳中继
│   ├── udp_relay.go           #   UDP 中继
│   └── session.go             #   UDP session 表
├── socks5/                    # SOCKS5 代理
│   ├── server.go              #   SOCKS5 服务器
│   └── handshake.go           #   SOCKS5 握手
├── node/node.go               # 顶层编排器
└── integration/               # 集成测试
    └── three_node_test.go
```

## 路由算法详解

采用 Path-Vector（路径向量）算法，类似 BGP：

1. 每个节点向所有直连 peer 通告自己直连的网段
2. 收到路由通告后，检查 AS-Path 是否包含自己（防环）
3. 如果路由表中没有该目标，或新路由更优，则更新
4. 更新后将路由通告给其他 peer（split-horizon：不传回来源）

**选路优先级**：最长前缀匹配 → LocalPref 高 → AS-Path 短 → Cost 低 → 时间戳旧

## 传输协议

节点间使用 TCP+TLS 长连接，通过自定义帧协议实现多路复用：

```
帧格式: [1B type] [4B streamID] [4B length] [length bytes payload]

Stream 0: 控制通道（gossip + 路由通告）
Stream N: TCP 数据中继（每 SOCKS5 连接一个 stream）
```

## 测试

```bash
make test
```

集成测试覆盖：
- 3 节点 gossip 收敛
- 2 节点 TCP relay 端到端数据转发

## License

MIT
