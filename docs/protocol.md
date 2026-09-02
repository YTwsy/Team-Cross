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
  capabilities: ["view", "annotate"] # 默认；明确开启控制时才追加 send/steer/interrupt
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
导入 stored Session、Continue、Fork、离线包导入/导出、反馈包导出、创建 Share 和撤销 Share。
远端响应会移除 repository root、
worktree path、当前 invitation、主机凭据与主机诊断路径。

## Session 审阅与内容范围

新增 host-only API：

| 路由 | 请求 | 结果 |
| --- | --- | --- |
| `POST /api/v1/sessions/preview` | `{provider, sessionId}` | SessionSnapshot 候选；不持久化、不执行 |
| `POST /api/v1/threads/from-session` | `{provider, sessionId, title?}` | 201，只读 ThreadDetail |
| `POST /api/v1/threads/{id}/sessions/import` | `{provider, sessionId, title?}` | 200，追加快照/Round；不创建 Run |
| `GET /api/v1/threads/{id}/feedback` | 无 | 带稳定引用的 Markdown 反馈，不发送 Agent |

`provider` 在历史读取中只允许 `codex`、`claude`。SessionSnapshot 包含
`id, threadId, source, capturedAt, entries, truncated, warnings, capabilities`。
`source` 保存 Provider 原始 `sessionId`、`identityKind`、`surface` 和可用的
`providerVersion`；Codex 对话身份使用 `thread.id`，不能使用 tree root 替代。
`entries` 的每项具有稳定 `id, kind, text` 和可选 `role, sourceId, turnId`。

ThreadDetail 增加 `readOnly` 和 `sessionSnapshots[]`。能力分别为
`read, follow, open, resume, takeControl, reason?`；历史读取成功不启用其他能力。
Session-first Thread 不自动使用来源 cwd，没有明确 Git baseline 时不能执行。

创建 Share 请求：

```json
{
  "ttlSeconds": 3600,
  "allowDegraded": false,
  "allowControl": false,
  "scope": {
    "snapshotId": "snapshot-uuid",
    "entryIds": ["entry-id"],
    "evidenceIds": [],
    "includeCode": false,
    "includeEvents": false
  }
}
```

scope 与邀请的 `scope: "collaborate"` 不同，前者是服务端存储的内容 allowlist。未提供
scope 或空列表表示不分享对应内容。code 是创建 Share 时最新 sealed Round 的代码，
不是实时 worktree；events 是另行授权的 managed Agent 实时输出，可能包含代码与
工具结果。控制要求 `includeCode`、`includeEvents` 同时启用，且 capability 允许写入。

详情、列表、Evidence 下载、patch、批注、SSE 都使用同一个投影。原始
`agent_transcript` Evidence 不可直接分享，应先导入结构化 SessionSnapshot。后续导入、
代码编辑与封存不会扩大已分享快照。活动 Share 再次创建返回 `409 share_active`，
必须先撤销再改变范围。SSE 也检查到期/撤销。

批注请求增加 `target`，恰好选择一类：

- `{snapshotId, entryId?}`：整个快照或其中一项消息/工具结果；
- `{evidenceId}`：一个 Evidence；
- `{roundId}`：不可变 Round，可配合原有 `file, line`。

指定文件且有 target 时必须是 Round target。远端目标必须在 Share scope 内；部分
分享的 snapshot 不允许以整个 snapshot 为目标。旧无锚点评论不会自动公开。批注不改写
Round，仍受下述 command/revision 语义限制。

## Round 继续与离线 Fork

以下路由只接受执行主机 Owner，不可经 Share 调用：

| 路由 | 请求 | 结果 |
| --- | --- | --- |
| `POST /api/v1/threads/{id}/continue` | `{roundId, provider, prompt, networkEnabled, expectedRevision, fork?, title?}` | 创建新 Session，返回 ThreadDetail |
| `POST /api/v1/threads/{id}/fork` | `{roundId, title?}` | 201，新 Thread；零执行 |
| `POST /api/v1/threads/{id}/bundles` | `{roundId, evidenceIds:[], snapshotIds:[], confirmExport:true}` | `application/vnd.teamcross.bundle+json` 附件 |
| `POST /api/v1/bundles/import` | 离线包 JSON 本体，非文件路径 | 201，新 Thread；零执行 |

Continue 的 provider 支持 `mock`、`codex`、`claude`。历史 Round 自动 Fork；
最新 Round 的完整 worktree 不符合封存状态时返回 `409 worktree_diverged`，不得覆盖。
`expectedRevision` 是必填字段；在调用 Provider 前持久化消费，不确定结果不得自动重发。
新 Run 准备失败不破坏旧 Run/Round；激活后的发送失败需 Owner 刷新再处理。

离线包格式 `teamcross.offline-fork`、version 1、文件后缀 `.tcx.json`；包含
`origin, baseline, objectFormat, gitObjects, snapshot, context, evidence, sessionSnapshots, objects`。
JSON bytes 使用 base64，Git 对象使用其原生哈希，CAS 使用 SHA-256。
接收端验证对象闭包、路径和物化预算后，创建独立对象库及隔离 worktree，并以事务保存
新的 Thread/Round 与来源映射。原 event seq、租约、凭据和进程不转移。

包包含 baseline 可达 Git 历史；显式选中的上下文成为不可撤回副本，Share 撤销不能删除。
v1 限制及新旧 schema 行为见
[审阅与接力契约](agent-wiki/sources/decisions/session-review-and-continuation.md)。
完整性校验不等于签名或可信指令；导入内容始终不自动执行。

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

未授权实时 managed 输出的 Share 会把事件投影成仅含 revision 的
`thread.updated`，保留本机 cursor/time，不透出隐藏内容、内部 ID 或错误信息。

join proxy 会先恢复私有 transport，再让浏览器的 EventSource 重连。因此 SSE 从哪个
cursor 继续，与新的底层连接最终选择 LAN、Tailnet 还是 Tailcat 无关。

## 兼容性边界

Tailcat v0.4.0 的 `connBlob` 被视为 opaque data。Team Cross 不解析或改写它；升级
Tailcat、InvitationV1 或 Share API 前，必须重新运行协议单元测试、自动 fallback 测试
和两台 Mac 验收。当前不存在服务端协议协商或云端 rendezvous。
