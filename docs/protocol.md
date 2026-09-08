# 实验接口

这些接口仅用于 `codex/session-collaboration`，不兼容旧 Thread/Share API。

## 本机管理 API

默认 `http://127.0.0.1:43210/api`。仅接受 loopback Host；浏览器写入需同源。响应为 JSON，错误返回 `{ "error": "可读说明" }`。接收端也必须在自己的 Mac 运行 Core，由它连接远端。

| 方法与路径 | 含义 |
| --- | --- |
| `GET /info` | 客户端位置、版本、主机、MCP 配置状态 |
| `POST /settings` | 保存 `binary`、`desktopApp` |
| `POST /mcp/setup` | 写入一次性 Codex MCP 配置 |
| `GET /sources?search=&cursor=` | 分页搜索原生来源会话 |
| `POST /preview` | 检查来源的最新完成轮与 Git 起点 |
| `GET /collaborations` | 本机发起与加入的协作 |
| `POST /collaborations` | 创建新的协作 fork |
| `POST /join` | `{invitation}` 连接一个邀请 |
| `GET /collaborations/:id` | 状态、目录、输入者、批注 |
| `GET /collaborations/:id/context?kind=&path=&after=&cursor=` | `history/changes/file/events` |
| `POST /collaborations/:id/action` | `{action,epoch}` |
| `POST /collaborations/:id/open` | `{client:tui|desktop,launch:boolean}` 直接客户端 |
| `POST /collaborations/:id/assist` | 同上，打开用户自己的客户端 |
| `POST /collaborations/:id/rpc` | `{method,params,requestId}` 协作原生调用 |
| `POST /collaborations/:id/respond` | `{id,result}` 原生请求回应 |
| `POST /collaborations/:id/annotations` | `{text,reference?}` |

创建及预览输入：

```json
{
  "sourceId": "来源原生会话 UUID",
  "workspaceMode": "existing",
  "requestId": "本次创建 UUID",
  "title": "自动生成且可编辑",
  "previewHash": "创建时回传预览哈希"
}
```

预览返回 `source`、`sourceTurnId`、`workspace`、`targetDirectory`、`previewHash`。不接受 dirty patch 或未跟踪文件选项。创建哈希绑定来源会话、完成轮、模式、目录、Git HEAD 和分支；未提交文件只是原目录当前现场，不捕获为快照。

`action` 包括：`start` 恢复运行时，`share` 生成邀请，`end` 结束共享，`handoff` 交给接收者，`reclaim` 发起者接回，`return` 接收者交还，`leave` 接收者离开。输入交接需要当前 `epoch`。

## LAN 分享

邀请格式 `tcx2.<base64url(JSON)>`，版本 `2`，能力 `codex-collaboration-v2`。含协作 ID、显示名称、主机、候选 IP/端口、SHA-256 SPKI 指纹、随机 secret 和到期时间。客户端以指纹验证 TLS 主机，使用 `Authorization: Bearer <secret>`。

| 路径 | 功能 |
| --- | --- |
| `GET /v2/status` | 此协作状态，不返回邀请 secret |
| `GET /v2/context` | 此协作上下文 |
| `POST /v2/rpc` | 有输入归属检查的原生请求 |
| `POST /v2/respond` | 原生审批/输入回应 |
| `POST /v2/annotations` | 添加批注 |
| `POST /v2/return` | 接收者交还输入 |
| `GET /v2/connect` | 原生 WebSocket upgrade |

TUI/Desktop 使用本机代理的根 WebSocket 地址；远端 TLS 路径和凭据由本机 Core 管理。只读请求失败可重新查询候选地址；写入失败不自动重放。邀请过期或权限失效要求重新分享。

## 状态与事件

协作状态 `preparing/ready/error`，接收者还可为 `left/ended/expired`。`online` 表示运行时是否连接，`busy` 表示轮次是否运行，`approvals` 表示待回应数量，`sharing` 表示共享是否开启，`connected` 表示是否已有直接客户端。

`context?kind=history` 返回 `{thread,nextCursor}`；`thread.turns` 是最近一页的至多 8 轮，页内按时间正序排列。传回非空 `nextCursor` 到 `cursor` 可读取更早的一页；`null` 表示没有更多历史。元数据和分页读取均不创建或执行轮次。

`context?kind=events&after=N` 返回 `{events,cursor,approvals,busy,online}`。每个事件有 `sequence/method/params/time`；保留最近 600 项。长期对话以原生历史为准，跨服务重启不要把旧事件 cursor 当作永久日志位置。`get_collaboration` 的 `sequence` 可判断当前游标是否重置。

原生写入请求必须有 `requestId`。一次原生客户端连接会得到新的连接标识，与客户端 RPC ID 一起构成写入 ID，连接断开后不会自动重新执行旧 RPC。`completed` 表示 RPC 得到响应，轮次最终结果需等待 `turn/completed`。

STDIO MCP 采用逐行 JSON-RPC 2.0，协议版本 `2024-11-05`；只在 stdout 输出协议消息。工具输入和结果遵循上述管理 API。

Desktop 的账户与偏好 RPC 在客户端本机分流，登录通知沿原客户端连接返回。A 的共享网关不支持远端修改主机账户，也不返回主机认证 token；`threadId/cwd/model/permissionProfile` 等共享执行参数由协作绑定。其他未开放的原生方法返回可读的“不支持”错误，不默认穿透。
