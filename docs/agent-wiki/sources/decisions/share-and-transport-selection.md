---
title: Share 与 LAN、Tailnet、Tailcat 连接选择
kind: decision
status: accepted
---

# Share 与 LAN、Tailnet、Tailcat 连接选择

## 决策

每次创建 Share 都启动独立的临时 runtime，并生成一个同时携带 LAN、可选 Tailnet 和
可选 Tailcat 候选的 `tcx1` 邀请。接收端固定按 LAN → Tailnet → Tailcat 分阶段拨号，
连接成功后由本地 join proxy 代理浏览器访问。

管理员 API 不参与 Share listener，也不直接暴露 Codex app-server、Claude SDK、Shell、
SQLite 或主机设置。

## 原因

LAN 是同地协作中成本最低、延迟最小的路径；已有 Tailnet 时可以复用长期设备身份与 ACL；
没有长期配对时，Tailcat 提供临时 direct/DERP 路径。把三者统一进一条邀请，接收者无需
理解或手工选择网络细节。

浏览器只连接 loopback join proxy，可以避免把 Share secret、SPKI pin 和 Tailcat
`connBlob` 暴露给普通 Web 环境。

## 不变量

- Share 绑定单一 Thread，默认一小时有效，最长 24 小时。
- 每个 Share 使用随机 `shareId`、32 字节 secret、临时 Ed25519 证书与 SPKI hash。
- LAN 同时使用邀请内 endpoint 和 mDNS；mDNS 失败不能阻断已知地址。
- 主机和接收端各自通过 Tailscale LocalAPI 判断 Tailnet 是否可用；一端未 Running 时
  不尝试该阶段。
- Tailcat v0.4.0 的 `connBlob` 是 opaque data，只由 Adapter 传递。
- TCP candidate 必须继续通过 SPKI pin 和 Share handshake。
- timeout、refused、ACL blocked 继续 fallback；正确指纹主机返回过期、撤销、认证失败
  或协议不兼容时停止。
- 一次成功连接不热迁移 transport；断线重连才从 LAN 重新开始。
- 主机重启后所有 Share runtime 失效，不能从 SQLite 恢复 secret listener。

## Tailcat 预热失败

默认创建 Share 时先预热 Tailcat。预热失败不会静默生成看似具备完整 fallback 的邀请。
只有主机在 WebGUI 明确允许 degraded Share 时，才生成只包含 LAN/Tailnet 的邀请。

## 重新打开条件

只有在引入账户、长期身份、云 rendezvous 或浏览器原生 transport 后，才重新讨论邀请
结构与拨号顺序。任何变化都需要新的协议版本与两台 Mac 回归。

## 事实来源

- `internal/invite/invite.go`
- `internal/share/runtime.go`
- `internal/share/gate.go`
- `internal/transport/`
- `internal/server/share.go`
- `internal/server/join.go`
- `docs/protocol.md`
