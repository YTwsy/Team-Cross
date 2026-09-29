# 验证与验收导航

当前应执行的命令、测试文件及环境要求，以 [验证门槛](../../sources/validation/test-gates.md) 为准。这页用于快速选择证据，避免把某一层的成功扩大为完整产品验收。

| 证据 | 能支持的判断 |
| --- | --- |
| 构建、类型检查与静态检查 | 当前代码与资源可编译，未代替运行时验证 |
| 单元、集成与 race test | 覆盖对应自动化用例中的逻辑和并发路径，模拟运行时不代表真实模型 |
| 真实浏览器与截图 | 页面流程、状态和视觉表现，未代替原生客户端执行 |
| 专用 TUI/Desktop 与原生运行时 | 对应版本、入口和测试目录中的真实行为 |
| 同机两个 Core 或两个客户端 | 本机 A/B 流程，未证明跨设备网络 |
| 两台 Mac 的 LAN | 实际网络条件下执行过的协作路径 |

GitHub `CI` workflow 自动执行 Go/Web 与 Homebrew 定义工程门槛；unsigned release workflow 可以手动生成经完整安装与 Homebrew 检查的 arm64 正式版或 RC artifact，也可以从 `origin/main` 上的同版本 annotated tag 生成 provenance，并把 RC 发布为 Pre-release、正式版本发布为 Latest。Release 成功后，Homebrew workflow 从公开资产复核 checksum、manifest、attestation 与隔离安装，再以最小权限 GitHub App 创建独立 tap PR；tap 的受保护检查、自动合并和公共 `main` smoke 全部成功后才回写 Homebrew 已发布。稳定版使用无后缀定义，RC 使用显式独立定义。上述流程都不自动运行真实模型、Tailcat 公网 smoke 或两台 Mac 网络验收，也不把稳定版本、Latest、Homebrew 可安装或 artifact provenance 当作 Developer ID 签名或 Apple 公证。

## 本次结果与历史记录

小改动在提交或 PR 中说明实际检查即可；跨会话调查、多环境验证或需要交接时，再按 [任务记录规则](../../README.md#任务记录) 保存报告。记录被测版本、环境、结果、证据位置及未覆盖项。

已结束记录保存在 `docs/agent-wiki/tasks/finished_archived/`，默认不搜索或批量读取，也不在本页逐条列举。需要追溯具体问题时定向读取；某个旧版本通过不能替代本次验证。可复用方法或通过标准发生变化时，更新当前验证门槛。

## 交付时记录

分别报告工程检查、浏览器检查和真实客户端检查；标明实际版本、环境及未覆盖范围。真实模型只使用专用 Luna 会话和仓库；验证后精确关闭本次测试客户端、Core、app-server、浏览器和残留辅助进程。

仅整理文档时检查链接、旧引用、代码路径和索引可达性即可，不重复启动这些运行时。只有证据类别或当前阅读入口变化时，才更新本页与 [任务索引](../index.md)；普通执行记录不要求同步导航。

相关任务：[目录](workspace-and-lifecycle.md) · [原生客户端](native-clients-and-models.md) · [输入与共享](input-and-sharing.md) · [WebGUI/MCP](webgui-and-mcp.md)
