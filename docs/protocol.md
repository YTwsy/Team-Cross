# Team Cross 协议 v1

本文描述当前原型的邀请 wire format、Share listener 边界、远端 API、命令幂等与
SSE 重放契约。协议字段、HTTP 路由、事件名和固定值保持英文原样。

## 邀请

邀请使用 canonical CBOR 编码，再以 raw base64url 表示，并添加 `tcx1.` 前缀。

```text
InvitationV1 {
  version: 1
  shareId
  expiresAt
  serverSPKISHA256
  secret
  scope: "collaborate"
  capabilities: ["view", "annotate", "send", "steer", "interrupt"]
  lan: { mdnsInstance, endpoints[] }
  tailscale?: { dnsName?, endpoints[] }
  tailcat?: { connBlob, virtualPort, libraryVersion: "v0.4.0" }
}
```

解码器会拒绝：

- 重复 CBOR key；
- indefinite value；
- 未知字段或 tag；
- 非 canonical 的重新编码结果；
- 长度错误的 key；
- 重复 capability；
- 不支持的版本；
- 已过期的邀请。

`tailcat` 仅在主机预热成功时出现。若主机明确允许降级 Share，Tailcat 预热失败后
邀请可以只包含 LAN/Tailnet 候选。

## Share listener

每个 Share 独占一个随机 IPv4 端口、一张临时 Ed25519 证书、一个 32 字节 secret，
以及可选的 mDNS 广告和已预热 Tailcat server。listener 外层 gate 按以下顺序检查：

1. Team Cross protocol version；
2. 撤销状态和到期时间；
3. Share ID 与采用 constant-time comparison 的 bearer secret。

`GET /share/v1/handshake` 由 gate 直接响应。其余请求只会转交给绑定单一 Thread 的
API handler，不会进入本地管理员 API。

## 远端 API 子集

Share 远端表面只允许：

```text
GET  /api/v1/info
GET  /api/v1/threads                 # 只返回绑定的 Thread
GET  /api/v1/threads/{id}
GET  /api/v1/threads/{id}/events
GET  /api/v1/threads/{id}/patch
GET  /api/v1/threads/{id}/evidence/{evidenceId}
POST /api/v1/threads/{id}/annotations
POST /api/v1/threads/{id}/control
POST /api/v1/threads/{id}/agent/send
POST /api/v1/threads/{id}/agent/steer
POST /api/v1/threads/{id}/agent/interrupt
POST /api/v1/threads/{id}/agent/input
```

host-only route guard 会拒绝远端执行 capture、附加 evidence、切换 Agent、读取或
导入 stored Session、创建 Share 和撤销 Share。远端响应会移除 repository root、
worktree path、当前 invitation、主机凭据与主机诊断路径。

## 乐观写入与 fencing

所有远端写入都携带：

```json
{
  "commandId": "uuid",
  "expectedRevision": 12,
  "leaseEpoch": 4
}
```

Observer 创建 annotation 时使用 `leaseEpoch: 0`。Agent 控制命令要求未过期的租约，
且租约中的 participant ID 与 epoch 必须同时匹配。

服务端会在执行操作前声明一条 `(share_id, command_id)` 记录，并把 participant identity
纳入持久化请求语义：

- 同 participant 的完全相同请求重放时返回已持久化的完成或失败结论；成功重放返回当前
  Thread snapshot，但不会再次执行命令；
- 同一个 `commandId` 被不同 participant 或不同请求语义使用时返回冲突；
- `expectedRevision` 落后时拒绝写入；
- Controller 租约 epoch 已变化时拒绝 Agent 控制命令。

完全相同的已完成重放不重新检查当前 revision 或 lease，因为这些前置条件已经在首次执行
时被消费；新命令仍必须使用最新的 revision 与 lease epoch。

## 控制租约

同一个 Share 同时只能有一个远端 Controller。租约持续 60 秒，WebGUI 通常每 20 秒
续约一次。没有 Controller 时，第一位请求者获得租约；其他参与者仍保持 Observer，
可以继续查看和批注。主机 Owner 可以随时抢占或撤销远端租约。

## SSE 重放

每个持久化 event 都取得 SQLite autoincrement `seq`，payload 同时携带事务完成后的
Thread `revision`。SSE endpoint 接受 `Last-Event-ID` 或 `?after=`，同时存在时采用
较大的 cursor，并按序补发 durable event。

join proxy 会先恢复私有 transport，再让浏览器的 EventSource 重连。因此 SSE 从哪个
cursor 继续，与新的底层连接最终选择 LAN、Tailnet 还是 Tailcat 无关。

## 兼容性边界

Tailcat v0.4.0 的 `connBlob` 被视为 opaque data。Team Cross 不解析或改写它；升级
Tailcat、InvitationV1 或 Share API 前，必须重新运行协议单元测试、自动 fallback 测试
和两台 Mac 验收。当前不存在服务端协议协商或云端 rendezvous。
