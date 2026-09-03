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
GET  /api/v1/threads/{id}/sessions/snapshots/{snapshotId} # 仅获准的不可变投影
GET  /api/v1/threads/{id}/patch
GET  /api/v1/threads/{id}/rounds/{roundId}/code # 仅获准的 sealed code Round
GET  /api/v1/threads/{id}/evidence/{evidenceId}
POST /api/v1/threads/{id}/annotations
POST /api/v1/threads/{id}/control
POST /api/v1/threads/{id}/agent/send
POST /api/v1/threads/{id}/agent/steer
POST /api/v1/threads/{id}/agent/interrupt
POST /api/v1/threads/{id}/agent/input
```

host-only route guard 会拒绝远端执行 capture、附加 evidence、切换 Agent、读取或
导入 stored Session、启动/停止 Follow、打开原生 Session、Continue、后继基线预览/创建、Fork、离线包导入/导出、反馈包导出、创建 Share 和撤销 Share。
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
`providerVersion`；可选 `providerSource` 只保存有界、已知的 Provider 来源 discriminator，
不作为界面身份、Session 身份或能力授权。Codex 的 `vscode` 来源也可能由 Desktop 返回，
因此此类 `surface` 为 `unknown`，不能仅依据 token 声称来自 VS Code 或 Desktop；原生
界面筛选不得扩大成对底层来源 token 的猜测。旧数据没有此字段仍可读取，公开 Share
投影不携带它。Codex 对话身份使用 `thread.id`，不能使用 tree root 替代。
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
创建槽在 transport 预热前占用，并发创建返回 `409 share_starting`。撤销当前 Share
也可取消尚未发布的创建；迟到成功不能发布邀请。仅有持久 Share 记录不代表运行时已
发布，旧记录与旧 listener 不能访问新 scope。关闭中的服务拒绝新创建。

批注请求增加 `target`，恰好选择一类：

- `{snapshotId, entryId?}`：整个快照或其中一项消息/工具结果；
- `{evidenceId}`：一个 Evidence；
- `{roundId}`：不可变 Round；
- `{roundId, side:"old"|"new"}` 配合 `file, line`：准确 sealed code 文本行。

新建文件批注必须提供 Round target、side 和正行号，并匹配该 Round 结构化 diff 的可见行。
`GET rounds/{roundId}/code` 返回 `{roundId,baseline,patch,lines}`；每行包含 kind/text 与
可用的 oldPath/newPath/oldLine/newLine。Share 只返回冻结代码投影，不回源读取其他 Round。
远端目标必须在 Share scope 内；部分
分享的 snapshot 不允许以整个 snapshot 为目标。旧无锚点评论不会自动公开。批注不改写
Round，仍受下述 command/revision 语义限制。

## 原生只读 Follow（能力门控）

以下接口仅供主机 Owner；当前原生 Adapter 的 `follow` 仍为 `false`，因此不能通过
接口绕过真实来源验收。数据库或离线快照中的旧能力也不能授权本机读取器。

| 路由 | 请求 | 结果 |
| --- | --- | --- |
| `POST /api/v1/threads/{id}/follows` | `{snapshotId, confirmReadOnly:true}` | 201 SessionFollow；同 Thread/source 已有活动 Follow 时 200 返回原记录 |
| `DELETE /api/v1/threads/{id}/follows/{followId}` | 无 | 200，持久化停止 fence；重复停止保持相同 epoch |

ThreadDetail 的主机视图可带 `sessionFollows[]`：
`id, threadId, source, sourceSnapshotId, currentSnapshotId, state, epoch, updatedAt,
lastPolledAt?, reason?, gaps[]`。`state` 为 `active`、`retrying`、`stopped`。
opaque cursor 只保存在本机数据库与 Bridge RPC 中，不进入 WebGUI、Share、离线包或 SSE。

Bridge `sessions.poll` 请求为 `{provider:"codex", sessionId, cursor?, limit?}`，初次省略
cursor（不发送空字符串）。结果为 `{source, capturedAt, entries, cursor, reset, gaps,
truncated, warnings}`。entries 是稳定 ID 的 upsert；reset 只替换当前 Follow 视图。
初次捕获为最新尾部，不承诺完整历史。游标上限 128 KiB；Bridge 的 snapshot/poll
序列化结果按 UTF-8/JSON 转义后的实际字节限制在 6 MiB 内，超限明确截断，不撑破
JSONL 通道。Core 响应/累积快照上限 20 MiB；当前视图最多 5000 条且按 16 MiB
内容预算保留较新记录，裁剪有明确缺口标记。

内容变化时，cursor、不可变 SessionSnapshot、Round 和 `session.follow.updated` 同事务
提交；重复内容只更新读取时间/游标，不追加 Round。停止先持久化 epoch，再取消读取，
迟到结果不能写入。重启恢复已授权 reader，但先重新验证当前来源能力；断线失败保留
cursor，重试前再次核对，明确历史改写才 reset，旧快照与批注锚点始终不变。

Follow 不创建 Run、发送 prompt、Resume、订阅原生 Writer 或自动迁移 Git 状态。其
checkpoint 继承的是独立捕获的 Git 基线，不声称对应原生会话当时的代码。活动 Share
默认仍冻结；即使 `includeEvents:true` 也不开放原生 Follow，相关事件只投影为
`thread.updated`。仅下述独立 `nativeLive` 授权可公开后续原生窗口。

### 原生实时窗口的独立授权

创建 Share 的 `scope.nativeLive` 可选；其形状为：

```json
{
  "followId": "follow-uuid",
  "expectedSnapshotId": "previewed-snapshot-uuid",
  "entryKinds": ["message"],
  "confirmCurrentAndFuture": true
}
```

类别只允许非空、无重复的 `message`、`tool`、`notice`。授权包括当前预览窗口及此后
同一 Follow 的选定类别，不自动脱敏文本。消息含人的输入和 Agent 回复，工具记录可能
含代码和路径；`includeCode` 只控制独立封存的 Git 材料。`includeEvents` 仍为 managed
事件授权，不是 `nativeLive` 的别名。该配置不授予任何 Follow 管理或 Agent 执行权限。

服务端持久绑定 Follow ID、epoch 和准确来源。Share 发布事务检查活动状态、已成功
读取及 `expectedSnapshotId`，与初始公开投影一同提交；变化返回冲突，要求刷新预览。
每次 Follow 提交原子登记新公开窗口。每个 Share 最多 128 个窗口/64 MiB 投影，达到
预算将公开状态置为 `limited`，不影响私有 Follow，不删除已经公开的锚点。

ThreadDetail 的 `nativeLive` 仅带 `{followId,state,latestSnapshotId,entryKinds,reason?}`，
state 为 `active/retrying/stopped/limited`。接收端不收到私有 `sessionFollows`。
详情只含静态选择和最新公开窗口；旧窗口通过
`GET /api/v1/threads/{id}/sessions/snapshots/{snapshotId}` 精确读取。该路由在主机读取
本 Thread 的快照，在 Share 端只能读取静态投影或已登记窗口；缺失/越权统一 404。
部分窗口不能建立整个 snapshot 批注，旧精准批注不随最新窗口更换原文。

SSE 的 `shared.session.updated` 只含 `{followId,snapshotId,state,revision}`；每个查询批次
轻量检查窗口 membership，发送每条事件前重新检查 Share 有效性，不读取快照正文。
发布事务记录本 Thread 的 event 起点；起点以前的事件保持通用通知，不被重放成后续
原生更新。超过预算时只能引用最后已公开 ID，不公开被限制的新 ID。
其他原生事件仍为通用 `thread.updated`。Share 撤销/到期停止读取、批注及持续 SSE；
Follow 停止后不再追加，新 Follow 不继承授权。已交付副本无法撤回。

### 精确打开原生会话（能力门控）

仅主机 Owner 可调用 `POST /api/v1/threads/{id}/sessions/open`，请求
`{snapshotId,confirmOpen:true}`。Core 重读指定来源并验证准确身份、界面、版本与
当前 `read/open`，不把旧快照能力当成授权，不创建 Run、Fork 或发送 prompt。

目标固定为 Codex Desktop，仅生成最小 UUID 深链。历史或当前 managed 同 ID、未确认
Writer/关闭状态和进行中的切换会返回 409；未验证来源/能力返回 422。成功 202 返回
`{status:"requested",target:"codex-desktop",provider:"codex",sessionId,message}`，只表示
系统接收请求，不保证原生 UI 显示或 CLI 终端恢复。此操作不是 Writer 交还。
生产能力仍关闭，必须分别完成 CLI/Desktop 真实验收才能启用。

## Round 继续与离线 Fork

以下路由只接受执行主机 Owner，不可经 Share 调用：

| 路由 | 请求 | 结果 |
| --- | --- | --- |
| `POST /api/v1/threads/{id}/continue` | `{roundId, provider, prompt, networkEnabled, expectedRevision, fork?, title?}` | 创建新 Session，返回 ThreadDetail |
| `POST /api/v1/threads/{id}/fork` | `{roundId, title?}` | 201，新 Thread；零执行 |
| `POST /api/v1/threads/{id}/successor/preview` | `{roundId, repo, untracked:[], goal, expectedRevision, title?}` | 代码与已保存审阅的 previewHash、baseline、数量；零执行 |
| `POST /api/v1/threads/{id}/successors` | 同预览请求并增加 `{previewHash, confirmSeparateBaseline:true}` | 201，独立后继 Thread；零执行 |
| `POST /api/v1/threads/{id}/bundles` | `{roundId, evidenceIds:[], snapshotIds:[], confirmExport:true}` | `application/vnd.teamcross.bundle+json` 附件 |
| `POST /api/v1/bundles/import` | 离线包 JSON 本体，非文件路径 | 201，新 Thread；零执行 |

Continue 的 provider 支持 `mock`、`codex`、`claude`。历史 Round 自动 Fork；
最新 Round 的完整 worktree 不符合封存状态时返回 `409 worktree_diverged`，不得覆盖。
`expectedRevision` 是必填字段；在调用 Provider 前持久化消费，不确定结果不得自动重发。
新 Run 准备失败不破坏旧 Run/Round；激活后的发送失败需 Owner 刷新再处理。

successor 仅适用于无 baseline 的只读审阅 Thread，必须明确指定本机 repo；不会采用原生
Session cwd。previewHash 绑定代码内容、来源 Round/快照/相关批注、目标、仓库与 revision，
创建时重新核对，变化返回 `409 successor_preview_changed`。新代码被明确标记为后续另行
捕获；来源 Round 不变。精确相关批注作为保留原锚点的 feedback Evidence 携带，不自动
改锚或迁移控制权。导入完成后需要再调用显式 Continue，不能沿用预览授权直接执行。

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

首次 command admission 是原子条件插入：同一 SQLite 写入检查 Share 未撤销/未过期、
所需 capability、participant、Thread revision，以及需要时的 lease。撤销先提交则迟到
HTTP body 不能新获执行资格；初始 HTTP 鉴权不算接受。此前已 admission 的操作可能完成，
撤销不承诺回滚已接受的工作。control.request 与 annotate 不要求事先持有 lease，
renew/release/Agent 控制需要准确 holder/epoch/expiry。
有效 Share/capability/member 下的 revision/lease 失败直接保存终态 error，不进入 running；
首次返回对应 fencing 错误，精确重放返回 `command_failed`。撤销/过期/越权不新建命令。

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
