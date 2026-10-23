# 驻留 MCP 与新版界面的升级衔接

2026-10-23 至 2026-10-24。来源为 `c7612dbb46bb0557ab178ca277cfd36e53d9cec1` 上 `codex/plugin-bridge-compatibility` 的本次改动，独立工作树；主工作区另有未提交的阅读界面工作，不进入本次范围。

## 问题与修复

已安装 App 和 Core 更新后，Codex 保留的旧 `teamcross mcp --ui` 进程仍执行旧路由白名单。旧面板从新 Core 热加载成员动态界面，访问 `activity` 和 `follows` 时被旧进程拒绝，显示 `WebGUI route is not allowed`。

本次增加独立于 bundle 传输格式的累积桥接能力：驻留 MCP 的 bootstrap 返回 `bridgeProtocol`，界面声明 `requiredBridgeProtocol`。新版加载器在下载及替换前检查，新入口在挂载业务页面前再检查。超出能力时保留原编辑器并提示保存未发送内容、退出并重新打开 Codex 或 ChatGPT；不提供无效的页面刷新按钮。首次加载被拦截时，提示采用宿主主题，中英文沿用 Core 偏好。

真实旧 MCP 还有一个兼容差异：bootstrap 不接受 body。新入口仅针对这次只读能力探测的确切路由拒绝，回退一次无正文 bootstrap，得到历史能力 0 后停止业务启动；不把该回退推广到业务接口。新 MCP 在启动 Core、写加载回执之前拒绝超出能力的 bootstrap。

旧加载器不认识新元数据时仍可能先替换脚本。新入口的启动拒绝会触发原加载器恢复编辑器；提示独立于 React 保留，沿用现有 banner 样式。已经运行故障界面的旧加载器回退后，仅把确切的路由拒绝改为重新加载说明，不隐藏其他错误、不修改草稿。

协议与维护规则见[协议](../../sources/protocol.md)、[本机插件契约](../../sources/decisions/chatgpt-local-plugin.md)和[验证门槛](../../sources/validation/test-gates.md#chatgpt-本机插件)。

## 验证

环境：macOS 14.8.5 arm64，Go 1.27.1，Node 24.18.0 / pnpm 11.19.0；生产构建的自包含面板、真实旧 stdio 与隔离 Core。宿主消息封装为测试夹具，iframe 禁止直接联网，不把 Core token 交给浏览器。没有启动真实模型，也没有重启用户的 Codex、ChatGPT、App 或 Core。

| 检查 | 结果 |
| --- | --- |
| `go test ./...`、`go vet ./...` | 通过 |
| `go test -race ./internal/mcp ./internal/collab ./internal/pluginpack ./internal/service ./internal/mcpassets` | 通过 |
| Web check / test / build | 1265 条翻译检查、25 个文件 238 项测试通过，两套嵌入资源已重建 |
| 协议回归 | 历史缺省、未来需求、带正文 bootstrap 回退、加载回执保护、下载前与替换前拒绝、草稿检查点保留及新连接清除提示通过 |
| 跨版本热更新 | 真实旧 PID 88279 在两次 Core bundle 切换时保持不变；原始路由错误复现，修复入口拒绝后恢复原草稿 |
| 旧加载器超时之后 | 从首次确认草稿与提示到再次截图间隔 134.241 秒，草稿和提示稳定，无循环替换 |
| 重新加载连接 | 仅夹具重载为新 MCP PID 93878；动态正常返回，重载提示消失 |
| 首次打开即不兼容 | 中文浅色 360 px 与英文深色 768 px；只发两次 bootstrap（旧 body 拒绝及无正文探测），没有业务读取或写入，没有无效刷新按钮 |
| 草稿与清理 | 未提交测试草稿；批注仍为 25 条，fork 数为 0，未启用共同执行；本次所有 stdio/Core/HTTP 夹具退出，浏览器测试页关闭 |

旧 helper 来自 `0.2.6-dev.workspace-plugin.20261023`（`8bc8397702bdc7ef6911678a2c6b9e726ad173dc`）；故障界面来自 `0.2.6-dev.member-activity.20261023`（`c7612db`）。二进制 SHA-256、进程证据、调用摘要、观察时间与清理结果见 [browser-validation.json](evidence/browser-validation.json)；工程门槛摘要见 [checks.json](evidence/checks.json)。

热更新阶段存在两次旧界面自动已读请求，均被旧 MCP 拒绝；重载后有一次正常的已读同步。它们与草稿提交分开记录，不能将整个场景描述成“零写入”。冷启动拦截场景则确实只有 bootstrap 探测。

截图：[原始错误](evidence/reproduced.png)、[跨超时后的草稿](evidence/preserved-draft.png)、[空间概览的恢复说明](evidence/overview-guidance.png)、[新 MCP 动态恢复](evidence/recovered.png)、[中文窄屏](evidence/cold-zh-360.png)、[英文深色](evidence/cold-en-dark-768.png)。中文热更新路径复核后仅补充首次加载的主题处理，并以真实旧进程复验英文深色；最终工程门槛针对包含该补充的构建执行。

## 交付与边界

提交后快进合入本地 `Meta`，从干净提交构建 `0.2.6-dev.plugin-bridge.20261024`。最终提交、build number、DMG 校验及安装验证由产物目录中的 `release.json`、`SHA256SUMS` 和 `validation.json` 记录；不创建远端发布或修改用户安装。

本次证明真实旧 MCP 路由与新 Core/生产界面组合下的兼容保护，宿主封装为模拟，不能表述为用户正在运行的 Codex 主窗口已完整验收。驻留旧进程的代码无法被网页热替换，用户仍需保存未发送内容后重新打开宿主。草稿保留承诺仅覆盖同一 iframe 的拒绝与回退，不承诺退出宿主后的草稿持久化。
