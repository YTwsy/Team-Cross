# 插件启动旧 Core 与接收助手路径恢复

日期：2026-10-04。被测代码为 `af9f715d02c0e7f94e50e1770ad42a524c92afbe` 上本报告所在提交的产品差异，分支 `codex/fix-plugin-core-upgrade`，目标 `Meta`。环境：单台 Apple Silicon Mac，macOS 14.8.5 / 23J423，Codex CLI `0.160.0`。

## 现场与修复

先前 CLI 发现修复已进入 `Meta`，但插件可以先启动自己复制的旧 Core；App 只检查控制协议兼容，继续复用旧实例。单独替换 App 或手工重启不能解决下次由插件启动的问题。同一开发版本号也不能区分两次构建。

插件稳定入口现记录所属安装来源，每次新启动转入当前 App/CLI。Core 启动器读取磁盘上的版本和 commit，空闲时协调替换；运行安装负责该数据目录，另一 CLI 副本不能来回切换来源。升级保留地址、目录和当前客户端设置，已有活动延后升级。旧版首次迁移使用其已有的非强制停止；新的请求准入保护只适用于支持新升级入口的 Core。DMG 可以支持此流程，无需为此切换 PKG。

用户截图中的两条助手记录均为旧路径错误、空原生 ID；当前 Core 的 CLI 发现已成功。因此助手列表另修复持久化错误投影：每次读取当前有效路径，路径恢复后显示恢复提示；明确重试复用原记录，只允许尚未发送 `thread/start` 的失败。发送前持久化阶段，未知结果不能重放。缺少 CLI 的新创建不分配失败卡片。

长期规则分别维护于 [分发](../../sources/distribution-and-onboarding.md)、[插件](../../sources/decisions/chatgpt-local-plugin.md)、[接收会话](../../sources/decisions/space-workbench.md) 和 [验证门槛](../../sources/validation/test-gates.md)。

## 工程结果

| 检查 | 结果及范围 |
| --- | --- |
| `go test ./...`、`go vet ./...` | 通过 |
| `go test -race ./internal/service ./internal/pluginpack ./internal/collab ./internal/mcp` | 通过 |
| Web check / test / build | 1002 条消息检查，163 项测试通过；两套嵌入资源已更新 |
| 接收助手路径回归 | 同应用旧路径恢复、设置变化、只读无写入/无原生会话、原 ID 明确重试、成功去重、丢失创建响应及重启不重放通过；原生 RPC 使用替身，无模型 |
| 生命周期脚本 | 并发启动、数据目录隔离、异常退出恢复、错误身份/协议拒绝与无客户端启动通过 |
| 插件连接脚本 | 真实 CLI 与独立配置目录；当前 Core 绑定、来源与资源 hash、无变化不同步、旧/新 bootstrap 回执、无关配置保留通过；无模型 |
| 浏览器与截图 | 按用户要求省略；本次后端修复及附带状态交互由上述检查覆盖 |

## 真实旧 Core 升级回归

`scripts/verify-core-upgrade.py` 使用三个隔离二进制和新的数据目录。旧版来自 `d1b3452f8f98decf69c8cba3d5316a717ad4252a`；新/下一构建使用测试标记 `plugin-core-upgrade-test` / `plugin-core-upgrade-next`，三个版本号均为 `0.2.6-dev`。这些标记是测试构建标识，不是 Git SHA。

最终参数交接回归结果保存于本地 `output/core-upgrade/roundtrip-settings/report.json`。以下断言均通过：

- 旧插件先启动后重现旧路径错误；五个并发启动者收敛到一个新 Core。
- 保留浏览器地址、原有数据、repo/loopback 参数；升级时采用后来保存的 CLI 设置，不带回旧启动参数。
- 原有旧 MCP 保持存活；再次启动不能复活旧 Core。
- 后续替换 App 而不重新同步插件，稳定入口和长驻 MCP 均使用磁盘新构建。
- 缺少所属安装时拒绝运行旧副本；未完成投递延后升级。
- 错误实例、缺少认证和浏览器 Origin 不能调用升级入口。
- CLI 恢复到当前应用内 `CodexCLI.app/Contents/MacOS/codex`，版本 `0.160.0`；不启动模型。

脚本关闭自身 Core 与 stdio。macOS 路径别名 `/var` 与 `/private/var` 的初版回归曾失败，修正为规范化身份比较后通过。接收助手 HTTP 测试的初版 fixture 缺少 loopback Host，修正测试 URL 后通过；生产同源规则未放宽。

需求收窄前已完成一轮专用 Luna 空间工作台检查，记录在本地 `output/core-upgrade/space-assistant/evidence/report.json`；所有自身进程已关闭。之后的助手卡片修复仅使用路径发现和模拟原生 RPC 验证。该真实模型结果不作为 CLI 修复的验收前置，也不扩大到用户的现有空间、任意 Desktop 或两台 Mac。

合入后的 DMG 来源、签名边界和 SHA-256 由构建目录的 `release.json` / `SHA256SUMS` 固定；本报告不把工作树测试标记当作最终安装包来源。Release 完成后不另行下载公开产物复核，规则已写入 [发布流程](../../sources/releasing.md)。
