# Share 与连接选择

当任务涉及 InvitationV1、Share listener、mDNS、Tailscale、Tailcat、TLS pin 或 join proxy
时，先读本页与根 `docs/protocol.md`。

## Share 创建

每次创建 Share 都生成新的 `shareId`、secret、临时 Ed25519 证书、SPKI hash 和到期时间，
再启动一个随机 IPv4 端口。这个 listener 只挂载绑定 Thread 的 Share API。

主机枚举 RFC1918 LAN 地址并发布 `_teamcross._tcp.local.`，同时把 endpoint 直接写入
邀请。Tailscale LocalAPI 只有在 Running 时贡献 Self Tailscale IP。Tailcat listener 在
返回完整邀请前预热，`connBlob` 原样写入邀请。

## 接收端三阶段选择

```text
LAN（1.5 s） → Tailnet（3 s） → Tailcat（12 s）
```

- LAN 阶段让 mDNS browse 与邀请 endpoint 共同参与，候选并发竞速。
- Tailnet 阶段要求接收端自己的 LocalAPI 也为 Running。
- Tailcat 作为最后一级，可能走 direct，也可能由底层选择 DERP relay。

候选 TCP 连接建立后，仍需验证 SPKI pin，并用 share ID/secret 完成 handshake。不能把
“端口可达”记录为成功。

## 失败分类

继续 fallback：网络 timeout、connection refused、路由不可达、Tailnet ACL 阻断。

立即停止：已经命中邀请 SPKI 的主机返回 invitation expired、share revoked、401/403、
protocol incompatible。继续尝试其他 transport 只会掩盖真实授权状态。

## join proxy

接收者浏览器只连接本机 loopback proxy。proxy：

- 保存 invitation secret 和 participant identity；
- 处理 pinned TLS 或 Tailcat connection；
- 给上游请求添加 participant name/ID 与 transport；
- 在 transport 断开后重新从 LAN 开始择路；
- 让浏览器继续使用同一个本地 origin 和 SSE cursor。

不要让浏览器直接消费 `connBlob` 或把远端私网证书降级为普通公网 PKI 校验。

## 主机重启

Share runtime 是临时能力。数据库中的历史 share/participant/event 用于审计，但进程重启
后不能重新发布旧 secret、证书或 listener。用户必须创建新邀请。

## 相关来源

- `../../sources/decisions/share-and-transport-selection.md`
- `docs/protocol.md`
- `internal/invite/`
- `internal/share/`
- `internal/transport/`
- `internal/server/join.go`
