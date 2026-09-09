# 运行时架构

Go Core 负责协作与本机管理；A 的专属 Codex app-server 持有共享原生会话。B 的本机 Core 连接 A，并为本机直接客户端和 MCP 提供入口。完整数据流见 [架构](../../sources/architecture.md)，字段和方法见 [协议](../../sources/protocol.md)。

## 从哪里进入代码

| 职责 | 入口 |
| --- | --- |
| 命令、监听、Web 嵌入与关闭 | [cmd/teamcross/main.go](../../../../cmd/teamcross/main.go) |
| 创建、记录持久化、恢复与进程管理 | [collab/app.go](../../../../internal/collab/app.go) |
| 原生 WebSocket RPC 和请求分发 | [nativecodex/process.go](../../../../internal/nativecodex/process.go) |
| 本机管理、共享与接收端代理 | [http.go](../../../../internal/collab/http.go)、[network.go](../../../../internal/collab/network.go) |
| 工具、工作目录与 TLS | [mcp](../../../../internal/mcp/)、[workspace](../../../../internal/workspace/)、[sharing](../../../../internal/sharing/) |

## 修改时守住的边界

读取来源可以按需启动控制进程，但不得发送业务 prompt。创建使用原生 fork；恢复使用已保存会话 ID。客户端断开和结束共享都不等于删除会话或目录。

数据目录沿用 `Team Cross Next`，与旧产品分开；变更默认路径要考虑已有协作可见性。协作记录使用 JSON，不能重新接回旧 SQLite/CAS、Node Bridge 或附加 `--native` 启动模式。

直接客户端与 MCP 共用协作的上游控制入口；账户与偏好按本机路由处理。不要把连接存在、RPC 成功、轮次完成或远端共享开放混成一个状态。

修改启动、恢复或并发状态后，按 [验证门槛](validation-gates.md) 完成 Go 测试与相关 race test。

相关任务：[原生客户端](native-clients-and-models.md) · [输入与共享](input-and-sharing.md) · [目录与生命周期](workspace-and-lifecycle.md)
