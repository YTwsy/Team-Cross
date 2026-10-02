# 前期本机验证快照

这里保存 2026-10-01 的纯本机调用探针，以及 2026-10-02 的真实 Core 读材料、写回复探针源代码和选取的非敏感证据。它们是当时版本的历史验证材料，不是成品插件的安装入口，也不证明后续提交自动通过。

两个旧包的 `.mcp.json` 包含当时本机验证目录和网络限制。复跑须生成新的配置并绑定新的隔离 fixture，不要将旧路径作为当前安装路径。`core_fixture_test.go.txt` 是原 collab 包的外部 overlay 源快照；以 `.txt` 保存以避免被 `go test ./...` 当作独立 Go 包编译。`native_rpc.py` 提供 stdio app-server 客户端，允许显式传入当前 CLI 与测试 cwd。

成品的可复用入口为 [verify-chatgpt-plugin.py](../verify-chatgpt-plugin.py)、[独立浏览器宿主](../chatgpt-plugin-browser.py) 与 [专用 Core fixture](../../internal/collab/plugin_fixture_test.go)。历史桌面回执对应 [10 月 1 日记录](../../docs/agent-wiki/tasks/finished_archived/2026-10-01-chatgpt-local-plugin-probe.md) 与 [10 月 2 日 Core 记录](../../docs/agent-wiki/tasks/finished_archived/2026-10-02-chatgpt-core-desktop-probe.md)。

`evidence-2026-10-02/` 只保留本机回执、网络范围和精确收尾结果；不包含 Core token、用户登录凭据或真实空间邀请。
