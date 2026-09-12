# 协作接口

这些接口是 `main` 的默认协作协议，不兼容旧 Thread/Share API。

本页是字段、路由与协议行为的统一说明。产品语义见 [词汇与核心模型](product-core-and-glossary.md)，调用入口见 [运行时架构](../wiki/concepts/runtime-architecture.md)。

## 本机管理 API

默认尝试 `http://127.0.0.1:43210/api`，占用时使用动态 loopback 端口，实际地址由本机连接文件和启动输出提供。仅接受 loopback Host；浏览器写入需同源。响应为 JSON，错误返回 `{ "code": "英文类型", "error": "可读说明", "recovery": "下一步（可选）" }`。接收端也必须在自己的 Mac 运行 Core，由它连接远端。

| 方法与路径 | 含义 |
| --- | --- |
| `GET /info` | 客户端位置、版本、主机、MCP 配置状态 |
| `POST /settings` | 保存 `binary`、`desktopApp`、`claudeBinary` |
| `POST /mcp/setup` | `{provider:codex|claude}` 写入个人 MCP 配置，省略默认 Codex（稳定 opt/App 路径） |
| `POST /mcp/probe` | 运行独立 STDIO 握手与工具枚举探测，不启动模型 |
| `POST /mcp/observed` | 本机凭据保护，`{provider:codex|claude}` 分别记录实际工具调用时间；未知客户端不记入 |
| `GET /control/status` | 本机凭据保护，返回实例、版本、控制协议及活动协作数，不启动 Codex |
| `POST /control/stop` | 本机凭据保护，`{force}`；活动协作未确认返回 409 |
| `POST /invitations/pending` | 本机凭据保护，`{invitation}` 暂存并返回不含 secret 的随机 ID |
| `POST /invitations/preview` | `{invitation}` 或 `{pendingId}`，只解析显示信息，不连接远端 |
| `GET /sources?provider=codex|claude&search=&cursor=` | 分页搜索原生来源会话 |
| `POST /preview` | 检查来源的最新完成轮与 Git 起点 |
| `GET /collaborations` | 本机发起与加入的协作 |
| `POST /collaborations` | 创建新的协作 fork |
| `POST /join` | `{invitation}` 或 `{pendingId}`，连接邀请并幂等返回本机加入记录 |
| `GET /collaborations/:id` | 状态、目录、输入者、批注 |
| `GET /collaborations/:id/context?kind=&path=&after=&cursor=` | `history/changes/file/annotations/events` |
| `POST /collaborations/:id/action` | `{action,epoch}` |
| `POST /collaborations/:id/open` | `{client:tui|desktop,launch:boolean}` 直接客户端 |
| `POST /collaborations/:id/assist` | `{provider:codex|claude,client:tui|desktop,launch:boolean}`，Provider 选择个人客户端，省略默认 Codex；Claude 仅 TUI |
| `POST /collaborations/:id/rpc` | `{method,params,requestId}` 协作原生调用 |
| `POST /collaborations/:id/respond` | `{id,result}` 原生请求回应 |
| `POST /collaborations/:id/annotations` | `{text,target?,reference?}` |

创建及预览输入：

```json
{
  "provider": "codex",
  "sourceId": "来源原生会话 UUID",
  "workspaceMode": "existing",
  "requestId": "本次创建 UUID",
  "title": "自动生成且可编辑",
  "previewHash": "创建时回传预览哈希"
}
```

预览返回 `source`、`sourceTurnId`、`workspace`、`targetDirectory`、`previewHash`。不接受 dirty patch 或未跟踪文件选项。创建哈希绑定来源会话、完成轮、模式、目录、Git HEAD 和分支；未提交文件只是原目录当前现场，不捕获为快照。

`action` 包括：`start` 恢复运行时，`share` 生成邀请，`end` 结束共享，`handoff` 交给接收者，`reclaim` 发起者接回，`return` 接收者交还，`leave` 接收者离开，`request_input` / `cancel_input` 接收者申请或取消输入。申请同样校验 epoch，不自动交接。输入交接需要当前 `epoch`。

## LAN 分享

原始邀请格式 `tcx2.<base64url(JSON)>`，App 链接包装为 `teamcross://join?invite=<URL 编码的原始邀请>`，版本 `2`，能力 `codex-collaboration-v2-membership`。含协作 ID、显示名称、主机、候选 IP/端口、SHA-256 SPKI 指纹、随机 secret 和到期时间。客户端以指纹验证 TLS 主机。`expiresAt` 只约束首次加入，以墙钟比较；旧能力邀请码明确拒绝，双方需使用本版。

`GET /v2/invitation` 与 `POST /v2/join` 使用 `Authorization: Bearer <invite.secret>`。B 在加入前生成并持久化 32 字节随机 `credential`，A 首次接受后绑定该凭据并消费邀请码；相同凭据显式重试加入幂等。其他 `/v2/*` 路由均使用 `Bearer <credential>`，不接受邀请码或依据邀请码时限失效。TLS 验证以 SPKI pin 为准，不把证书日期作为成员到期时间。

| 路径 | 功能 |
| --- | --- |
| `GET /v2/invitation` | 验证未使用邀请并发现地址，不读取协作上下文、不加入 |
| `POST /v2/join` | `{credential}`，一次性确认加入；不同凭据不能重复使用同一邀请 |
| `POST /v2/leave` | 主动离开，撤销成员凭据、关闭远端直接连接并将输入归还 A |
| `GET /v2/status` | 此协作状态，不返回邀请 secret |
| `GET /v2/context` | 此协作上下文 |
| `POST /v2/rpc` | 有输入归属检查的原生请求 |
| `POST /v2/respond` | 原生审批/输入回应 |
| `POST /v2/annotations` | 添加批注 |
| `POST /v2/return` | 接收者交还输入 |
| `POST /v2/presence` | `{online}`，Core 心跳或本机退出；10 秒发送 / 30 秒在线窗口，不决定成员资格 |
| `POST /v2/request_input` | `{epoch}`，申请输入 |
| `POST /v2/cancel_input` | `{epoch}`，取消输入申请 |
| `GET /v2/connect` | 原生 WebSocket upgrade |

TUI/Desktop 使用本机代理的根 WebSocket 地址；远端 TLS 路径和凭据由本机 Core 管理。只读请求失败可重新查询候选地址；写入失败不自动重放。未使用邀请到期、成员主动离开或共享结束后需新邀请；B 的 Core 重启从持久化凭据重连，不重新加入。

## 状态与事件

协作状态 `preparing/ready/error`，接收者还可为 `joining/left/ended`。`online` 表示运行时是否连接，`busy` 表示轮次是否运行，`approvals` 表示待回应数量，`sharing` 表示共享是否开启，`connected` 表示是否已有直接客户端。`participantOnline` 表示参与方 Core 的有效心跳，`inputRequested` 表示有效输入申请；两者与直接客户端连接独立。`clientState` 为 `disconnected/connected/session_ready`，最后一种必须由该直接连接成功读取或恢复绑定的 thread 确认。

`participantJoined` 独立于在线心跳；`invitationState` 为 `pending/joined/expired/left`，只有 `pending` 状态向 A 返回邀请码和 `expiresAt`，B 状态不带加入期限。`runtimeState` 为 `running/starting/releasing/released/offline`，`releasePending` 表示共享关闭后仍在等待工作或客户端结束。退出过程完成后才显示 `released`；恢复等待旧进程完全退出。

`context?kind=history` 返回 `{thread,nextCursor}`；`thread.turns` 是最近一页的至多 8 轮，页内按时间正序排列。传回非空 `nextCursor` 到 `cursor` 可读取更早的一页；`null` 表示没有更多历史。元数据和分页读取均不创建或执行轮次；会话释放后通过只读控制进程读取，不 `thread/resume`、不重新占用原生写入锁。

`context?kind=events&after=N` 返回 `{events,cursor,approvals,busy,online}`。每个事件有 `sequence/method/params/time`；保留最近 600 项。长期对话以原生历史为准，跨服务重启不要把旧事件 cursor 当作永久日志位置。`get_collaboration` 的 `sequence` 可判断当前游标是否重置。

原生写入请求必须有 `requestId`。一次原生客户端连接会得到新的连接标识，与客户端 RPC ID 一起构成写入 ID，连接断开后不会自动重新执行旧 RPC。`completed` 表示 RPC 得到响应，轮次最终结果需等待 `turn/completed`。

`info.mcpClients.codex|claude` 各含 `configured`、`command`、`configError?`、`observedAt`；配置检测限定本机辅助目录。`mcpProbed` 是共用工具服务的独立协议检查。兼容 CLI 诊断的顶层 `mcpConfigured/mcpCommand/mcpObservedAt` 仍对应 Codex。实际调用根据 MCP 初始化声明的客户端名识别，未知名不冒充 Codex；该信息只用于诊断，不授予协作权限。

STDIO MCP 采用逐行 JSON-RPC 2.0，协议版本 `2024-11-05`；只在 stdout 输出协议消息。工具输入和结果遵循上述管理 API。

Desktop 的账户与偏好 RPC 在客户端本机分流，登录通知沿原客户端连接返回。A 的共享网关不支持远端修改主机账户，也不返回主机认证 token；`threadId/cwd/permissionProfile` 等共享执行参数由协作绑定。其他未开放的原生方法返回可读的“不支持”错误，不默认穿透。

## 模型设置

协作状态包含运行时确认的 `model`、`modelProvider` 和可空的 `reasoningEffort`；未知值不填造默认模型。离线返回最近保存的设置。

原生网关支持 `thread/settings/update` 的模型与推理设置。它和带配置覆盖的 `thread/resume` 属于写入，需要当前输入者及 `requestId`；只读恢复查询不能绕过输入归属修改模型。`turn/start` 保留 `model/effort`，不指定时沿用当前会话。`thread/resume.config` 仅保留模型与推理相关配置。

## 批注引用

`Annotation` 包含 `id/text/author/createdAt`，可带 `target`。`reference` 只是人工参考说明，不参与自动定位。正文最多 4000 字；整体意见不传 `target`。主机生成作者、ID、时间并绑定所属协作的 `sessionId`，拒绝其他会话的定位。

`target` 的公共字段为 `kind`、`sessionId`、`quote`。`quote` 保存批注时所选原文，最多 8000 字。不同目标使用以下字段：

| kind | 定位字段 | 原文与版本含义 |
| --- | --- | --- |
| `history` | `turnId/itemId/startOffset/endOffset/cursor?` | 消息正文的 UTF-16 偏移，左闭右开；`cursor` 为读取该页时使用的游标，最近页省略 |
| `file` | `path/startLine/endLine/contentHash` | `path` 相对 A 的 `executionCwd`；行号从 1 开始且含首尾；`quote` 为完整行，不带结尾分隔换行；`contentHash` 是读取到的整个文件的 SHA-256 |
| `changes` | 同 file，另有 `side/baseRevision` | `side=old` 指向 `baseRevision` 的旧行，`side=new` 指向该次 diff 的新行；`contentHash` 是整个返回 diff 的 SHA-256，`quote` 不含 diff 的增删前缀 |

`context?kind=file` 返回 `{path,text,contentHash}`。`kind=changes` 返回 `{stat,status,diff,contentHash,baseRevision,truncated}`；diff 路径相对执行目录，限制 256 KiB 并在完整行截断，`truncated` 表示只返回部分内容。`baseRevision` 为该次 diff 使用的具体 HEAD，不使用协作创建时的 HEAD 代替。

`context?kind=annotations` 返回 `{annotations,sessionId,executionCwd}`，本机和 LAN 路由相同，MCP `read_context` 支持该 kind；`get_collaboration` 同样返回批注。`add_annotation` 支持完整的 `target` 对象。工具描述说明如何用 path、消息 ID 和历史游标读取原文，并提醒旧行属于基准提交。

位置和 `contentHash` 是客户端提供的阅读快照，主机检查字段格式、相对路径、范围与片段长度，不将其作为已验证的当前代码事实，也不会保存时改写成新文件的指纹。文件可在编辑批注期间变化；处理前通过 Team Cross 读取 A 上的实际上下文，再核对片段与版本，不能把 A 上路径当成 B 本机同名文件。指纹不同不代表该片段一定变化，但不能据此直接高亮旧位置。消息按稳定 ID 和精确片段判断；未定位时继续保留引用。

保存和读取批注不调用 `turn/start`、`turn/steer`，不改变输入者；写入失败不自动重放。保存到磁盘失败时撤回内存追加，避免显示未持久化的成功结果。成员权限沿用共享路由校验，结束共享后拒绝远端继续读写。

## 维护入口

批注实现见 [annotation.go](../../../internal/collab/annotation.go)、[上下文组件](../../../packages/web/src/components/Context.tsx) 和 [批注组件](../../../packages/web/src/components/Annotations.tsx)。

路由与转发以 [本机 HTTP](../../../internal/collab/http.go)、[共享连接](../../../internal/collab/network.go)、[原生 RPC](../../../internal/collab/rpc.go)、[邀请与 TLS](../../../internal/sharing/sharing.go) 和 [STDIO MCP](../../../internal/mcp/server.go) 为准。协议变化在同一提交中更新本页及对应测试；Wiki 页面引用本页，不另存一份路由表。

## 后台控制与错误分类

控制协议版本为 1，与 LAN v2 独立。`connection.json` 包含 `url/pid/instance/token/version/commit/protocol/dataDir`，0600 原子写入；公开状态省略 token。控制接口拒绝 Origin 并校验本机 Bearer token。普通页面继续通过现有同源检查访问管理接口。

主要错误类型包括 `invitation_invalid`、`invitation_used`、`membership_invalid`、`invitation_expired`、`invitation_pending_expired`、`sharing_ended`、`host_unreachable`、`version_incompatible`、`instance_mismatch`、`client_missing`、`mcp_not_configured`、`input_changed`、`active_collaborations`。未分类错误为 `operation_failed`。远端错误保留 code/recovery，写入失败不自动重试。

首次体验与生命周期详见 [分发与首次体验](distribution-and-onboarding.md)。

## 命令入口与版本诊断

`GET /info` 的 `version` 是当前运行 Core 的版本，`installedVersion` 是稳定安装位置的可执行文件报告的版本；读取失败返回空值，不把运行版本当作已安装版本。二者不同时，设置页提示退出并重新打开以应用更新。

`cli` 包含 `executable/target/command/source/installed/canInstall/canRemove/conflict/pathReady`；`source` 为 `app/formula/standalone/unavailable`。命令定位与归属检查只读，不修改 shell 配置。`pathReady` 只反映进程实际继承的 PATH；Finder 下额外检查 Homebrew 位置不代表终端已配置这些目录。

App helper 的 `cli-status`、`install-cli`、`uninstall-cli` 支持 `--json` 与绝对路径 `--cli-dir`，在 Core 启动逻辑之前处理。只有后两种命令可由菜单栏请求系统授权。冲突、权限不足与临时挂载 App 分别返回 `cli_conflict`、`cli_permission_denied`、`cli_app_not_installed`；独立 CLI 调用安装操作返回 `cli_requires_app`。

## Claude 实验性协议

`provider` 省略时为 `codex`，另接受 `claude`；其他值拒绝。创建请求的幂等比较包含 Provider；Claude 的预览哈希额外绑定来源 JSONL 指纹。`GET /info` 增加 `claudeBinary/claudeVersion/claudeError`。完整运行时契约见 [Claude 原生 TUI](decisions/claude-native-tui.md)。

Claude 状态返回 `provider:claude`、`nativeJobId`、`nativeWaiting`，以及 `capabilities`：`nativeTui/sendInput=true`，`nativeDesktop/steerInput/interruptTurn/respondToRequest=false`。`approvals` 不包含 Claude TUI 内部的待审批请求；不能据此推断没有等待交互。模型与推理强度来自最近持久化的原生 assistant 消息，尚未执行下一轮时不提前宣布 TUI 设置已确认。

同一 `/v2/connect` 根据协作 Provider 分发。Claude 首帧为 JSON `{op:"attach",cols,rows,attachId,caps}`；A 返回原生连接 ACK，之后 binary 帧是终端字节，text 帧仅允许同一 `attachId` 的 resize。不存在任意原生命令穿透。成员认证、单个直接客户端、输入归属和 epoch 继续生效。`session_ready` 要求原生 ACK nonce 对应且包含已加载消息的 `content_paint`。

Claude 控制 RPC 支持绑定会话的 `thread/read`、`thread/turns/list`、`thread/list`、`thread/loaded/list`、`thread/unsubscribe` 和空闲时 `turn/start`。后者只接受一段非空、至多 256 KiB 的文本，不接受模型覆盖；返回 `{accepted:true,provider:"claude"}` 表示 worker 接收，不伪造 turn ID。补充、中断、审批和设置方法返回 `native_client_required`。历史分页中 `turn.id` 是原生用户消息 UUID，供阅读与批注定位，不作为原生控制用 turn ID。

Claude 事件使用 `teamcross/claudeState`、`teamcross/claudeHistory` 与已确认的 `thread/settings/updated`；状态轮询不是 Codex 的 `turn/completed` 流。控制端需要同时读状态和历史确认结果。原生 TUI 原始输入没有请求 ID 或逐包执行确认，连接层不会缓存或重放；它不提供 RPC 的幂等接收承诺。
