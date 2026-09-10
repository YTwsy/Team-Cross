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

读取来源可以按需启动控制进程，但不得发送业务 prompt。创建使用原生 fork；恢复使用已保存会话 ID。共享开放时客户端断开保留运行时；共享结束且执行、审批、请求和直接连接清空后，关闭此协作的 app-server 释放原生锁。只读历史不恢复该会话，显式恢复继续同一 ID。两者都不删除会话或目录。

数据目录沿用 `Team Cross Next`，与旧产品分开；变更默认路径要考虑已有协作可见性。协作记录使用 JSON，不能重新接回旧 SQLite/CAS、Node Bridge 或附加 `--native` 启动模式。

直接客户端与 MCP 共用协作的上游控制入口；账户与偏好按本机路由处理。不要把连接存在、RPC 成功、轮次完成或远端共享开放混成一个状态。

修改启动、恢复或并发状态后，按 [验证门槛](validation-gates.md) 完成 Go 测试与相关 race test。

相关任务：[原生客户端](native-clients-and-models.md) · [输入与共享](input-and-sharing.md) · [目录与生命周期](workspace-and-lifecycle.md)

CLI、App、MCP 现在共用按数据目录发现的后台 Core；默认 serve 在后台运行，调试用 --foreground。实例身份、版本和受控停止见 [分发与首次体验](../../sources/distribution-and-onboarding.md)。

分发使用 App 内置 CLI：Cask 注册其命令，DMG 由菜单栏安装启动器，Formula 提供互斥的独立安装。命令安装仅维护自身入口，不启动或提权 Core；设置页区分磁盘安装版本与运行版本。修改命令归属时先读上述来源与 `internal/cliinstall`，不要在 Homebrew 之外覆盖其链接。
