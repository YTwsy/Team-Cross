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
| `POST /spaces` | `{title,requestId}`，幂等创建独立只读空间，不创建运行时 |
| `POST /publications/source` | `{provider,sourceId}`，只在本机冻结来源已结束历史 |
| `POST /publications/preview` | `{draftId,title,startTurnId,endTurnId,readingStartId?}`，生成所选范围的固定预览 |
| `POST /publications/draft` | `{draftId}`，读取本机冻结草稿；无共享端对应路由 |
| `POST /sources/current` | 核对个人 MCP 的原生调用身份，返回来源与当前轮次 |
| `POST /share-requests/preview` | 预览当前 Session 的本轮完成后分享 |
| `POST /share-requests` | 幂等登记分享请求，立即返回，不等待本轮结束 |
| `GET /share-requests/:id` | 查询请求状态、已创建协作 ID 和错误恢复提示 |
| `POST /share-requests/:id/cancel` | 只取消仍在 waiting 的请求，保留已创建资源 |
| `POST /preview` | 检查来源的最新完成轮与 Git 起点 |
| `GET /collaborations` | 本机发起与加入的协作 |
| `POST /collaborations` | 创建新的协作 fork；可用 `spaceId` 附到已有只读空间 |
| `POST /join` | `{invitation}` 或 `{pendingId}`，连接邀请并幂等返回本机加入记录 |
| `GET /collaborations/:id` | 状态、目录、输入者、批注 |
| `GET /collaborations/:id/context?kind=&path=&after=&cursor=` | `history/changes/file/annotations/events` |
| `POST /collaborations/:id/action` | `{action,transport?,epoch,memberId?}`；`share` 使用 `lan|tailcat`，省略仅为兼容本机调用并默认 `lan` |
| `POST /collaborations/:id/invitations` | `{transport,requestId?,reset?}` 生成或取回可复用链接；`reset:true` 配合 requestId 重置链接；也可只传 `{invitationId}` 取回指定链接 |
| `POST /collaborations/:id/execution-access` | `{memberId,allowed}`，主机开放或收回该成员的执行访问，保留材料与讨论资格 |
| `POST /collaborations/:id/revoke-invitation` | `{invitationId}`，关闭该链接加入，保留已加入成员 |
| `POST /collaborations/:id/remove-member` | `{memberId}`，撤销该成员及旧请求；当前输入者被移除时输入归还发起者 |
| `POST /collaborations/:id/open` | `{client:tui|desktop,launch:boolean}` 直接客户端 |
| `POST /collaborations/:id/personal-desktop` | `{launch:boolean}`，仅本机发起的 Codex 协作；在个人 Desktop 定位已保存的 fork |
| `POST /collaborations/:id/assist` | `{provider:codex|claude,client:tui|desktop,launch:boolean}`，Provider 选择个人客户端，省略默认 Codex；Claude 仅 TUI |
| `POST /collaborations/:id/rpc` | `{method,params,requestId}` 协作原生调用 |
| `POST /collaborations/:id/respond` | `{id,result}` 原生请求回应 |
| `GET /collaborations/:id/materials` | 材料与版本目录，不含正文 |
| `POST /collaborations/:id/materials` | `{previewId,previewHash,requestId,materialId?,baseVersion?}`，发布本机已确认预览 |
| `POST /collaborations/:id/read-material` | `{materialId,version,turnId?,cursor?}`，分页读取固定版本 |
| `POST /collaborations/:id/publication-status` | `{requestId}`，查询自己的发布结果 |
| `POST /collaborations/:id/withdraw-material` | `{materialId}`，撤回自己材料的所有版本 |
| `POST /collaborations/:id/annotations` | `{text,target?,reference?,materials?}` |
| `POST /collaborations/:id/annotation-replies` | `{annotationId,text,requestId,materials?}`，返回更新后的原批注 |
| `POST /runtime-annotations/:id` | 仅 A loopback，独立协作凭据，`{name,arguments}`，读取或回复本空间批注，按需读已发布材料 |

创建及预览输入：

```json
{
  "provider": "codex",
  "sourceId": "来源原生会话 UUID",
  "workspaceMode": "existing",
  "runtimeMode": "restricted",
  "requestId": "本次创建 UUID",
  "title": "自动生成且可编辑",
  "previewHash": "创建时回传预览哈希"
}
```

`runtimeMode` 为 `restricted|trusted`，省略默认为 `restricted`，未知值拒绝。Codex 与 Claude 均支持。它绑定预览哈希、创建请求去重与持久化协作记录，创建后没有修改入口；重试不能借同一个 requestId 改变模式。恢复继续同一 sessionId 与模式。信任模式继承当前主机原生配置，不是创建时配置文件的快照。

预览返回 `runtimeMode`、`source`、`sourceTurnId`、`sourceTurnStatus`、`workspace`、`targetDirectory`、`previewHash`。不接受 dirty patch 或未跟踪文件选项。创建哈希绑定 Provider、协作模式、来源会话、完成轮、目录模式、目录、Git HEAD 和分支；未提交文件只是原目录当前现场，不捕获为快照。Codex 来源有 rollout 路径时，以原始轮次事件核对运行/完成状态，避免独立读取进程将其他客户端仍在运行的轮次误判为中断。

`action` 包括：`start` 恢复运行时，`share` 按显式 `transport` 生成邀请，`end` 结束共享，`handoff` 交给接收者，`reclaim` 发起者接回，`return` 接收者交还，`leave` 接收者离开，`request_input` / `cancel_input` 接收者申请或取消输入。申请同样校验 epoch，不自动交接。输入交接需要当前 `epoch`。WebGUI 必须让用户在 `lan` 和 `tailcat` 之间二选一；协议默认 `lan` 只用于已有本机调用者，不表示自动探测或回退。

协作列表以本机记录和最近一次成功状态立即响应，不等待逐个远端主机重连；已加入协作的远端状态在后台去重刷新，下一次页面轮询会显示结果。`GET /collaborations/:id` 仍等待该协作的实时状态探测，供详情页和工具确认当前输入归属与运行状态。

`personal-desktop` 从本机持久化协作记录读取 `sessionId`，生成 `codex://threads/<sessionId>`，不使用来源 `sourceId` 或请求体提供的会话 ID/URL。返回 `{sessionId,url,command,launched,note}`；`launch:false` 仅生成命令，`launch:true` 通过 `open -a <Desktop路径> <URL>` 请求打开普通 Desktop，`launched` 只表示系统打开请求成功，不证明页面、侧边栏或新消息已刷新。此入口不要求当前输入权或在线运行时，不 fork、resume、发送 prompt、连接共享网关或更改共享状态；不适用于接收者或 Claude 协作。

## 共享邀请与传输

原始邀请格式 `tcx3.<base64url(JSON)>`，App 链接包装为 `teamcross://join?invite=<URL 编码的原始邀请>`，版本 `3`，能力 `collaboration-spaces-v3-links`。公共字段含空间 ID、显示名称、主机、`readOnly`、`runtimeMode`、`transport`、SHA-256 SPKI 指纹、随机 secret 和到期时间。只读邀请 `readOnly=true` 且不携带 `runtimeMode`；执行邀请 `readOnly=false`，模式省略为受限、未知值拒绝。连接后的能力以 A 的记录为准。`expiresAt` 为可选墙钟期限，省略或零值表示本次共享内无固定到期时间；`tcx2`、v1/v2 空间能力及其他旧能力明确拒绝，双方需使用兼容版本。

候选字段按传输互斥：

| `transport` | 邀请候选 | 建立方式 |
| --- | --- | --- |
| `lan` | `endpoints: ["private-ip:port", ...]`，不得包含 `tailcat` | 依次尝试候选 TCP 地址；生产只接受私有 IPv4，测试可显式加入 loopback |
| `tailcat` | `tailcat: {address,port:443,libraryVersion:"v0.6.0"}`，不得包含 `endpoints` | 解析完整 Tailcat 地址，要求精确库版本，通过 Tailcat `DialTCPPort(443)` 建立流 |

Tailcat 地址包含 WireGuard 节点材料和默认预共享密钥，整个邀请码都必须按秘密处理。服务端仅允许虚拟 TCP 443，其他端口在 Tailcat packet filter 与回调处拒绝。Tailcat 负责 DERP 引导、直连探测与必要时的中继；Team Cross 不根据路径自动切换传输，也不把 DERP 引导日志当作最终中继证据。

两种传输在其连接之上使用相同的 TLS 1.3 临时证书。客户端以邀请中的 SPKI 指纹验证主机，不使用系统 CA，也不把证书日期作为成员到期时间。

`GET /v2/invitation` 与 `POST /v2/join` 使用 `Authorization: Bearer <invite.secret>`。B 在加入前生成并持久化 32 字节随机 `credential`，A 接受后为每份独立凭据建立成员，链接继续接纳其他人；相同凭据显式重试加入幂等。其他 `/v2/*` 路由均使用 `Bearer <credential>`，不接受邀请码或依据邀请码时限失效。TLS 验证以 SPKI pin 为准，不把证书日期作为成员到期时间。

| 路径 | 功能 |
| --- | --- |
| `GET /v2/invitation` | 验证开放中的链接并发现地址，不读取协作上下文、不加入 |
| `POST /v2/join` | `{credential,name?}`，确认加入；同一链接接纳多个独立凭据，相同凭据的重试复用成员 |
| `POST /v2/leave` | 主动离开，只撤销本人凭据；若为当前输入者，关闭其直接连接并将输入归还 A |
| `GET /v2/status` | 此协作状态，不返回邀请 secret |
| `GET /v2/context` | 此协作上下文 |
| `POST /v2/rpc` | 有输入归属检查的原生请求 |
| `POST /v2/respond` | 原生审批/输入回应 |
| `POST /v2/annotations` | 添加批注 |
| `POST /v2/annotation-replies` | 回复已有原批注 |
| `GET /v2/materials` | 此空间的材料目录 |
| `POST /v2/materials` | `{content,requestId,materialId?,baseVersion?}`；只接收发布者本机选定后的内容，不读取其来源 |
| `POST /v2/read-material` | 按固定版本和公开范围读取 |
| `POST /v2/publication-status` | 查询当前成员自己的发布请求 |
| `POST /v2/withdraw-material` | 撤回当前成员自己的材料 |
| `POST /v2/return` | 接收者交还输入 |
| `POST /v2/presence` | `{online}`，Core 心跳或本机退出；10 秒发送 / 30 秒在线窗口，不决定成员资格 |
| `POST /v2/request_input` | `{epoch}`，申请输入 |
| `POST /v2/cancel_input` | `{epoch}`，取消输入申请 |
| `GET /v2/connect` | 原生 WebSocket upgrade |

TUI/Desktop 使用本机代理的根 WebSocket 地址；远端 TLS/Tailcat 路径和凭据由本机 Core 管理。LAN 只读请求失败可依次查询邀请候选，Tailcat 只按邀请地址重建客户端；写入失败都不自动重放。成员主动离开后可通过仍有效的链接重新加入；链接关闭、到期或整个共享结束后需有效链接；B 的 Core 重启从持久化凭据和原传输重连，不重新加入、不改选传输。

## 状态与事件

`hasExecution` 区分独立只读空间与带可操作会话的空间；`reachable` 表示本次空间探测是否成功，不能用运行时的 `online` 代替。只读空间没有 `sessionId/executionCwd/runtimeMode/writer/epoch` 等执行字段，`context` 仅接受 `kind=annotations`，原生 RPC、连接、恢复及输入申请拒绝。发布材料与批注不需要输入权。带执行的空间即使运行时离线，也可分享已保存材料和讨论。

持久化记录采用 `schema:2`，顶层为身份、批注和材料，原生字段收纳为可选 `execution`。旧扁平记录保留在磁盘但不加载或迁移。为已有空间创建执行，预览及创建绑定 `spaceId`，保留原链接与成员。只读成员保持原范围；主机通过 `execution-access` 明确授权原生历史与目录。每个空间最多一个执行，沿用固定模式及恢复规则。只读空间的 `action:end` 将 `state:ended` 持久化，即使尚未开放共享也有效；再次生成链接恢复为 ready。

`role` 仅区分本机发起者 `owner` 与加入者 `remote`；授权不使用共同的 `remote` 身份。`selfId` 为本机成员身份（发起者为 `owner`），`writer` 为 `owner` 或具体成员 ID。直接客户端和辅助工具比较 `selfId` 与 `writer`。`members` 返回 `{id,name,joinedAt,active,online,inputRequested,executionAccess}` 数组；每人凭据独立，显示名称允许重复，不用于授权或去重。`invitations` 仅向发起者返回各邀请的 `{id,state,expiresAt?,joinedCount}`，不含凭据。

`handoff` 的 `memberId` 指向已加入成员；多于一位成员时省略会拒绝，只有一位时可明确推导。已获执行访问的成员都能申请输入，其他成员持有输入时也能排队；不会自动交接，交给 C 不清除 B 的申请。离线只改变在线状态，主动离开或移除才撤销资格。

邀请 `requestId` 用于操作去重；普通调用始终取回当前链接，新 requestId 不新增单人邀请。`reset:true` 必须携带 requestId，重试返回同一次重置结果，旧链接立即停止新加入，已有成员保持有效。`invitationId` 只取回指定链接，不能与 requestId 或 reset 混用。参与端在同一空间和 TLS 指纹下复用已有身份，即使打开的是重置后的链接。更换传输前必须结束整个共享。


协作状态 `preparing/ready/error`，接收者还可为 `joining/left/ended`。`online` 表示运行时是否连接，`busy` 表示轮次是否运行，`approvals` 表示待回应数量，`sharing` 表示共享是否开启，`sharingPreparing` 表示所选传输仍在建立，`transport` 为当前或正在准备的 `lan|tailcat`，`connected` 表示是否已有直接客户端。`participantOnline` 表示至少一位成员 Core 的有效心跳；发起者的 `inputRequested` 表示任一成员申请，加入者的该字段只表示本人申请；两者与直接客户端连接独立。`clientState` 为 `disconnected/connected/session_ready`，最后一种必须由该直接连接成功读取或恢复绑定的 thread 确认。

`participantJoined` 独立于在线心跳；`invitationState` 为 `active/expired/revoked`，active 才返回当前链接。`invitationId` 标识当前链接，`invitationReadOnly` 表示链接授予的原始范围；`invitations` 中记录 `joinedCount`，不绑定单个 memberId。默认链接无固定时限，存在期限时才返回 expiresAt。`runtimeState` 为 `running/starting/releasing/released/offline`，`releasePending` 表示共享关闭后仍在等待工作或客户端结束。

`context?kind=history` 返回 `{thread,nextCursor}`；`thread.turns` 是最近一页的至多 8 轮，页内按时间正序排列。传回非空 `nextCursor` 到 `cursor` 可读取更早的一页；`null` 表示没有更多历史。元数据和分页读取均不创建或执行轮次；会话释放后通过只读控制进程读取，不 `thread/resume`、不重新占用原生写入锁。

`context?kind=events&after=N` 返回 `{events,cursor,approvals,busy,online}`。每个事件有 `sequence/method/params/time`；保留最近 600 项。长期对话以原生历史为准，跨服务重启不要把旧事件 cursor 当作永久日志位置。`get_collaboration` 的 `sequence` 可判断当前游标是否重置。

原生写入请求必须有 `requestId`，持久去重键包含成员 ID、原生方法和请求 ID；不同成员不复用写入结果，同一请求改变内容会拒绝。一次原生客户端连接会得到新的连接标识，与客户端 RPC ID 一起构成写入 ID，连接断开后不会自动重新执行旧 RPC。`completed` 表示 RPC 得到响应，轮次最终结果需等待 `turn/completed`。

`info.mcpClients.codex|claude` 各含 `configured`、`command`、`configError?`、`observedAt`；配置检测限定本机辅助目录。`mcpProbed` 是共用工具服务的独立协议检查。兼容 CLI 诊断的顶层 `mcpConfigured/mcpCommand/mcpObservedAt` 仍对应 Codex。实际调用根据 MCP 初始化声明的客户端名识别，未知名不冒充 Codex；该信息只用于诊断，不授予协作权限。

STDIO MCP 采用逐行 JSON-RPC 2.0，协议版本 `2024-11-05`；只在 stdout 输出协议消息。工具输入和结果遵循上述管理 API。

个人 MCP 提供 `request_input/cancel_input_request/handoff_input/reclaim_input/return_input`，参数为 `{id,epoch,memberId?}`（`memberId` 用于选择交接接收者），分别映射 `request_input/cancel_input/handoff/reclaim/return`。`epoch` 必须是刚查询到的正整数状态版本；缺失、过期、错误角色、尚未加入或忙碌交出/交还均拒绝。接回不自动中断轮次。成功后查询详情获取新版本，失败或超时不自动刷新版本重放动作。普通详情和输入管理结果均移除 `invitation`；工具错误以 JSON 文本返回 `code/error/recovery` 并设置 `isError:true`。

CLI `collaborations [--id <id>] [--json]` 查询列表或详情；`input request|cancel|handoff|reclaim|return --id <id> --epoch <版本> [--json]` 使用同一工具适配与 Core API。两者支持 `--data-dir`，按需启动或复用 Core，不打开浏览器。`status` 保持服务诊断语义，不能代替协作详情。

### 个人 MCP 管理入口

| 工具 | 参数与映射 |
| --- | --- |
| `list_source_sessions` | `{provider,search?,cursor?}` → `GET /sources`；只读取本机来源 |
| `preview_collaboration` | `{provider,sourceId,workspaceMode,runtimeMode?,title?,requestId?,spaceId?}` → `POST /preview`；省略 requestId 时生成 UUID，并在结果附带 `requestId/workspaceMode` |
| `create_collaboration` | 同预览，加必填 `requestId/previewHash` → `POST /collaborations` |
| `get_current_source` | 无参数；用原生调用身份 → `POST /sources/current` |
| `preview_current_share` | `{workspaceMode,transport,runtimeMode?,title?,requestId?}` → `POST /share-requests/preview`，返回 requestId 与 previewHash |
| `share_current_session` | 同上，加必填 `requestId/previewHash` → `POST /share-requests`；不接受 provider/sourceId/caller 工具参数 |
| `get_share_request` / `cancel_share_request` | `{id}` → `GET /share-requests/:id` / `POST /share-requests/:id/cancel` |
| `create_invitation` | `{id,transport,requestId?,reset?,invitationId?}` → `POST /collaborations/:id/invitations`；传输必须显式指定；开放链接增加 `invitationUrl` |
| `set_execution_access` | `{id,memberId,allowed}` → `POST /collaborations/:id/execution-access`；只允许发起者操作 |
| `preview_invitation` / `join_collaboration` | `{invitation}` → 预览 / 加入；不自动打开浏览器或原生客户端 |
| `open_client` | `{id,client,launch?}` → `/open`，默认 launch=true；TUI 使用新终端窗口 |
| `end_sharing` / `leave_collaboration` / `resume_collaboration` | `{id}` → `action:end/leave/start`，Core 检查本机角色 |
| `create_readonly_space` | `{title,requestId}` → `POST /spaces` |
| `freeze_source_session` | `{provider,sourceId}` → `POST /publications/source` |
| `preview_publication` | `{draftId,title,startTurnId,endTurnId,readingStartId?}` → `POST /publications/preview` |
| `publish_material` | `{id,previewId,previewHash,requestId,materialId?,baseVersion?}` → `POST /collaborations/:id/materials` |
| `get_publication_status` | `{id,requestId}` → `POST /collaborations/:id/publication-status` |
| `list_materials` | `{id}` → `GET /collaborations/:id/materials` |
| `read_material` | `{id,materialId,version,turnId?,cursor?}` → `POST /collaborations/:id/read-material` |
| `withdraw_material` | `{id,materialId}` → `POST /collaborations/:id/withdraw-material` |

来源 Provider 与个人辅助客户端独立。创建输入要求明确 Provider、来源和目录；预览与创建继续绑定固定模式和 Git 起点。工具校验参数类型、枚举和未知字段；共享运行时的内置批注 MCP 不增加这些管理工具。除明确邀请生成外，结果移除 `invitation`。当前开放链接在有人加入后仍可再次取回。

CLI `sources/preview/create/share/invite/inspect-invitation/open/end/leave/resume` 复用同一 MCP 工具适配；参数见根 README。`share` 要求已有确认的 previewHash 和 requestId，不暗中更新起点；先创建再邀请。邀请失败时输出 `{stage:"created",collaboration,invitationError,recovery}` 并以非零状态退出，继续用 `invite`。`open --print-command` 仅返回启动计划，不接管当前终端。`join --no-open --json` 在原有 URL 和服务输出之外返回本机协作 `id`。

### 当前 Session 与延后分享

MCP 根据 `initialize.clientInfo.name` 识别个人客户端。Codex 从 `tools/call.params._meta.threadId` 及 `x-codex-turn-metadata.thread_id/turn_id` 取得会话与轮次，冲突拒绝；Claude 从 MCP 子进程的 `CLAUDE_CODE_SESSION_ID` 及调用元数据 `claudecode/toolUseId` 取得身份，再在该 Session 的当前主链中核对工具调用。Claude 历史异步落盘允许最多 3 秒有界等待。不会采用可能继承自其他启动器的 `CODEX_THREAD_ID`、最近会话或模型自行提供的 ID。上述客户端字段属于按实际版本验证的接入条件，不承诺所有版本或 Desktop 一定携带；缺失时回到明确来源选择。

本机 HTTP 的 current 预览/提交是普通创建字段再加 `caller:{provider,sourceId,turnId?,toolUseId?}` 和 `transport`，MCP 适配层从传输生成 caller，Core 再核对来源与最新轮次。它表示调用来源，不授予超出本机管理接口的权限；共享运行时的批注 MCP 不暴露这些入口。

延后分享预览哈希绑定来源 Provider、Session、本轮 ID、固定模式、目录、Git HEAD/分支和传输。运行中的 Claude transcript 会继续追加工具结果，因此该意图哈希不绑定中途 JSONL 指纹；本轮完成后重新使用普通 Preview/Create 绑定完整最终指纹。Codex 读取原生 rollout 的 `session_meta`、`task_started/task_complete/turn_aborted`，核对 Session 和最新轮次；读取不 resume 来源。未知、损坏或超过 64 MiB 的历史拒绝创建。

请求状态为 `waiting → creating → inviting → ready`，另有 `cancelled/failed/interrupted`。同一 requestId 与相同预览返回原请求，参数变化拒绝；请求 ID 同时是拟创建的协作 ID。每个 Core 至多 32 个活动请求，单次请求限时 15 分钟；未完成请求计入 Core 停止保护。来源变成另一轮、Git 起点变化或本轮中断时失败，不自动选新起点或重复创建。`waiting` 可取消；已开始创建后不删除 fork/worktree。创建留下部分结果或邀请失败时，状态保留 collaborationId，先查询该协作，邀请只单独重试。

状态和错误原子写入 `share-requests/<id>.json`，不保存邀请 secret。Core 关闭会取消并等待工作线程；重启把未完成记录改为 interrupted，不自动重放。ready 表示该请求已完成创建和邀请，不承诺之后共享一直有效；Core 重启后仍按普通协作规则恢复、重新邀请。CLI `share-status/cancel-share --id <请求ID> [--json]` 使用同一查询/取消工具，不从 shell 环境猜当前来源。

Desktop 的账户与偏好 RPC 在客户端本机分流，登录通知沿原客户端连接返回。A 的共享网关不支持远端修改主机账户，也不返回主机认证 token；`threadId/cwd/permissionProfile` 等共享执行参数由协作绑定。其他未开放的原生方法返回可读的“不支持”错误，不默认穿透。

成员执行访问与空间成员资格独立。`executionAvailable` 表示空间已配置执行；远端 `hasExecution` 只在该成员获准时为 true。未获准的状态投影不含原生 ID、目录、模型、输入和运行时信息；历史、文件、改动、事件、RPC 与直接客户端均校验执行授权。带原生定位的批注及回复也只对获准成员可见，材料与普通讨论保持开放。收回执行访问取消该授权下的在途原生请求、关闭直接连接并接回输入；不移除成员或自动中断模型。

## 模型设置

协作状态包含运行时确认的 `model`、`modelProvider` 和可空的 `reasoningEffort`；未知值不填造默认模型。离线返回最近保存的设置。

原生网关支持 `thread/settings/update` 的模型与推理设置。它和带配置覆盖的 `thread/resume` 属于写入，需要当前输入者及 `requestId`；只读恢复查询不能绕过输入归属修改模型。`turn/start` 保留 `model/effort`，不指定时沿用当前会话。`thread/resume.config` 仅保留模型与推理相关配置。

## 已发布会话材料

冻结来源必须提供准确的 `provider/sourceId`；可使用来源列表，或先由个人 MCP `get_current_source` 核对当前原生身份，不能根据更新时间或目录猜测。冻结只读取已结束的连续历史前缀，不 resume、fork 或读取来源目录；正在运行的一轮留在本机。Codex 还以持久化轮次事件核对结束边界。草稿保存在本机 `publication-drafts/`，新对话不会追加进去。

预览在固定草稿中选择连续的 `startTurnId..endTurnId`，每轮包含可导出的用户提问、可见回复和已保存工具过程。`readingStartId` 仅为导航，必须在范围内。返回 `{id,hash,frozenAt,title,provider,sourceId,startTurnId,endTurnId,readingStartId?,turns}`；`turns` 为 `{id,status,items:[{id,type,text,notice?}]}`。隐藏推理不导出；未知内容、未导出的图片/附件与过长项目有明确说明，不凭路径或 URL 额外拉取内容。完整预览展示实际将上传的内容；折叠不等于隐藏。

本机发布端只接受预览 ID 和匹配的哈希，向托管端发送已选择的 `content`。主机以当前成员身份生成作者和版本，来源字段是材料的来源声明，不是跨主机的密码学真实性证明。同一作者的 `requestId` 与同一请求内容返回相同结果，参数变化拒绝；先保存再返回 `state:published/materialId/version/hash`。结果不明用 `publication-status` 查询，`not_found` 不能证明网络中原请求已失败。显式重试必须保持同一请求 ID、预览和基准版本。

更新只允许原作者及同一 Provider/来源，`baseVersion` 必须匹配当前版本。新版本保留旧版，不改变已有引用；目录的 `changes` 统计相对上一版新增、变化、移出的轮次。撤回停止空间提供该材料所有版本，不删除原生 Session，无法召回已读取的副本和历史引用。成员被移除后旧材料仍属于空间，但该成员凭据无法继续读写。成员 ID 在同次共享内稳定；结束共享后重新加入是新身份，当前不能用新身份修改旧成员的材料。

目录返回材料作者、标题、版本、哈希、轮数、公开起止、建议阅读起点及导出说明数量，不返回正文。`read-material` 必须指定正整数版本；返回轮次目录 `turns`、正文 `segments` 和 `nextCursor`。每段带 `turnId/itemId/type/status/text/notice/startOffset/endOffset`，偏移为原项目 UTF-16 位置。每页最多 16000 Unicode 字符、64 段；游标绑定材料、版本和内容哈希，服务端先检查它们属于当前空间，不允许跨对象使用。读取到公开末尾就结束，没有“继续读取私人历史”的路径。

当前原型读取来源上限为 2048 轮 / 16 MiB 原生结果，单个导出项目超过 256 KiB 时截成 64000 字符并注明；单次材料内容最多 4 MiB，每空间全部版本最多 64 MiB。保存失败回滚内存。JSON 记录以 `schema:2` 保存，不兼容旧扁平记录。

CLI 复用个人 MCP 适配：`space`、`freeze`、`publication-preview`、`publish`、`publication-status`、`materials`、`read-material`、`withdraw-material`。更新用 `publish --material <ID> --base-version <旧版本>`，分页用 `read-material --cursor <nextCursor>`；原生 `preview/create/share --space <空间ID>` 可为只读空间启用执行。完整发布示例见根 README。

## 批注引用

批注与回复均可附 `materials:[{materialId,version,turnId?}]`，最多 16 项，必须指向本空间未撤回的已发布固定版本。引用不会跟随最新版改变。材料撤回后保留历史讨论和当时引用，但不能继续通过空间读取正文。

`Annotation` 包含 `id/text/author/authorId/createdAt`，可带 `target` 和按保存顺序排列的 `replies`。`reference` 只是人工参考说明，不参与自动定位。正文最多 4000 字；整体意见不传 `target`。主机生成作者、ID、时间；原生上下文绑定所属执行的 `sessionId`，材料上下文绑定本空间的 `materialId/version`。

`target` 的公共字段为 `kind` 和 `quote`；原生类型另含 `sessionId`，材料类型禁止携带原生 `sessionId/cursor`。`quote` 保存批注时所选原文，最多 8000 字。不同目标使用以下字段：

| kind | 定位字段 | 原文与版本含义 |
| --- | --- | --- |
| `history` | `turnId/itemId/startOffset/endOffset/cursor?` | 消息正文的 UTF-16 偏移，左闭右开；`cursor` 为读取该页时使用的游标，最近页省略 |
| `material` | `materialId/version/turnId/itemId/startOffset/endOffset` | 固定发布版本的 UTF-16 偏移；服务端核对已保存正文和精确片段，不能回溯未公开内容 |
| `file` | `path/startLine/endLine/contentHash` | `path` 相对 A 的 `executionCwd`；行号从 1 开始且含首尾；`quote` 为完整行，不带结尾分隔换行；`contentHash` 是读取到的整个文件的 SHA-256 |
| `changes` | 同 file，另有 `side/baseRevision` | `side=old` 指向 `baseRevision` 的旧行，`side=new` 指向该次 diff 的新行；`contentHash` 是整个返回 diff 的 SHA-256，`quote` 不含 diff 的增删前缀 |

`context?kind=file` 返回 `{path,text,contentHash}`。`kind=changes` 返回 `{stat,status,diff,contentHash,baseRevision,truncated}`；diff 路径相对执行目录，限制 256 KiB 并在完整行截断，`truncated` 表示只返回部分内容。`baseRevision` 为该次 diff 使用的具体 HEAD，不使用协作创建时的 HEAD 代替。

`context?kind=annotations` 返回 `{annotations,sessionId?,executionCwd?}`，只读空间省略执行字段；本机、LAN 和 Tailcat 路由相同，MCP `read_context` 支持该 kind；`get_collaboration` 同样返回批注。`add_annotation` 支持完整的 `target` 对象。工具描述说明如何用 path、消息 ID 和历史游标读取原文，并提醒旧行属于基准提交。

位置和 `contentHash` 是客户端提供的阅读快照，主机检查字段格式、相对路径、范围与片段长度，不将其作为已验证的当前代码事实，也不会保存时改写成新文件的指纹。文件可在编辑批注期间变化；处理前通过 Team Cross 读取 A 上的实际上下文，再核对片段与版本，不能把 A 上路径当成 B 本机同名文件。指纹不同不代表该片段一定变化，但不能据此直接高亮旧位置。消息按稳定 ID 和精确片段判断；未定位时继续保留引用。

保存和读取批注不调用 `turn/start`、`turn/steer`，不改变输入者；写入失败不自动重放。保存到磁盘失败时撤回内存追加，避免显示未持久化的成功结果。成员权限沿用共享路由校验，结束共享后拒绝远端继续读写。

### 批注回复

`AnnotationReply` 为 `{id,requestId,text,author,authorId,createdAt,materials?}`，没有 `target`、`parentReplyId` 或子回复。`annotation-replies` 接受当前空间的原批注 ID、1–4000 字正文、至多 200 字节的非空 `requestId` 和可选材料引用，拒绝其他字段。主机生成作者、ID 和时间；同一作者的同一请求 ID 返回已保存结果，若原批注、正文或材料引用不同则拒绝。保存失败回滚内存，读取快照不会被并发回复原地修改。普通成员无需获得模型输入权即可讨论；结束共享后拒绝远端读写。完整个人 MCP 使用 `reply_to_annotation(id,annotationId,text,requestId,materials?)`。

### 共享运行时的批注工具

A 的 Codex app-server / Claude worker 通过 `teamcross mcp --data-dir … --runtime-id …` 启动 `teamcross_annotations`。枚举 `read_annotations(annotationId?)`、`reply_to_annotation(annotationId,text,requestId,materials?)`、`list_materials()` 和 `read_material(materialId,version,turnId?,cursor?)`。它们只访问绑定空间，不允许选择其他空间、枚举私人来源、发布材料、读取任意路径或发送模型输入。参数校验在 STDIO 和 Core 两端执行。读取批注返回原文引用和全部回复；回复作者由运行时 Provider 决定，为 `Codex` 或 `Claude Code`。

每个协作保存独立 `annotationToken`，仅通过 A 上的 MCP 环境变量传递；普通状态、远端响应和原生 `config/read` 不暴露凭据。STDIO 每次调用重新读取本机连接地址，只使用协作凭据，不启动 Core、不使用管理 token。创建、明确恢复或重新开启共享时开启批注工具访问，结束共享关闭访问；发起者恢复后即使尚未重新邀请，也可读取。运行时未连接、已释放或 Core 关闭时拒绝访问。成员访问撤销仍由原生入口和共享路由处理。

Codex 启动协作运行时前按所选模式读取有效 MCP 名称。受限模式在进程配置中逐项禁用继承的服务；信任模式保留这些服务。两者均追加批注服务，有同名个人条目时选用未占用的后缀名称，避免混入原传输和环境配置。命令行 MCP 表会跨层合并，不能依靠空表或新表覆盖来隔离。创建、恢复和 TUI/Desktop 的模型选择保留该进程配置，启动时不改写个人配置文件。Claude 受限模式使用 `--strict-mcp-config`；信任模式在个人 MCP 之外追加带协作 ID 的批注服务名称，原生恢复沿用已保存的启动配置。批注工具不注入业务 prompt；信任模式可能同时加载邀请者原有的完整个人辅助 MCP。此前创建的 Claude worker 仍沿用其旧启动参数；不改写原生 job 状态，使用新建协作接入。

Codex 信任模式支持原生 hook 确认：只含 `hooks.state` 或其子项的 `config/value/write`、`config/batchWrite` 由本机代理转发给 A，主机再次校验模式、输入归属及 `requestId`，移除客户端指定的 `filePath`；其他配置写入保持客户端本机路由。`hooks/list` 的 `cwds` 固定为执行目录。Team Cross 不自动确认 hook，也不把普通偏好写入升级为主机设置修改。

实现见 [运行时批注适配](../../../internal/collab/runtime_annotations.go) 和 [受限 STDIO 工具](../../../internal/mcp/runtime.go)。

## 维护入口

批注实现见 [annotation.go](../../../internal/collab/annotation.go)、[上下文组件](../../../packages/web/src/components/Context.tsx) 和 [批注组件](../../../packages/web/src/components/Annotations.tsx)。

路由与转发以 [本机 HTTP](../../../internal/collab/http.go)、[共享连接](../../../internal/collab/network.go)、[原生 RPC](../../../internal/collab/rpc.go)、[邀请与 TLS](../../../internal/sharing/sharing.go) 和 [STDIO MCP](../../../internal/mcp/server.go) 为准。协议变化在同一提交中更新本页及对应测试；Wiki 页面引用本页，不另存一份路由表。

## 后台控制与错误分类

控制协议版本为 1，与共享邀请 v3 独立。`connection.json` 包含 `url/pid/instance/token/version/commit/protocol/dataDir`，0600 原子写入；公开状态省略 token。控制接口拒绝 Origin 并校验本机 Bearer token。普通页面继续通过现有同源检查访问管理接口。

主要错误类型包括 `transport_invalid`、`sharing_preparing`、`sharing_cancelled`、`sharing_active`、`invitation_invalid`、`invitation_revoked`、`execution_access_required`、`membership_invalid`、`invitation_expired`、`invitation_pending_expired`、`sharing_ended`、`host_unreachable`、`version_incompatible`、`instance_mismatch`、`client_missing`、`mcp_not_configured`、`input_changed`、`active_collaborations`。未分类错误为 `operation_failed`。远端错误保留 code/recovery，写入失败不自动重试。

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

多人成员的个人 MCP 入口包括 `create_invitation`（支持 `requestId`、`reset` 与指定取回的 `invitationId`）、`remove_member`、`revoke_invitation` 和 `set_execution_access`。CLI 对应 `invite`，重置使用 `invite --reset --request-id <新ID>`，执行授权使用 `execution-access --id <空间> --member <成员> --allow=true|false`，以及 `remove-member --id <协作> --member <成员ID>`、`revoke-invitation --id <协作> --invitation-id <邀请ID>`；多人交接使用 `input handoff --id <协作> --member <成员ID> --epoch <版本>`。批注及回复由主机绑定 `authorId` 和显示名称，不采信请求提供的作者。
