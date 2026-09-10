# v0.1.1 分发验收 · 2026-09-11

## 范围与环境

对象为 `389608e7b54891f83e483abbcc669aa2c35e6e56` 基线上的 v0.1.1 分发、首次体验、持续加入资格、会话释放和上下文刷新实现。本页记录发布准备期间实际检查；本地开发包版本为 `0.1.1-dev`。最终版本包的完整提交、版本和校验值由 `release.json`、`SHA256SUMS` 记录，生成下载地址不代表已经公开发布。

环境：Apple Silicon、macOS 14.8.5（23J423）、Go 1.25.3、Node 24.18.0、pnpm 11.19.0、Swift 6.0.3、Homebrew 6.0.22。本轮真实运行时使用 Codex CLI 0.153.4，模型仅为 `gpt-5.6-luna`。已安装 Desktop 为 26.903.71938，但没有完成该版本的窗口操作验收。

## 工程与安装结果

| 检查 | 实际结果 |
| --- | --- |
| Go test / vet | `go test ./...`、`go vet ./...` 通过 |
| race | `collab/mcp/sharing/nativecodex/service/cliinstall` 通过 |
| Web | check、21 项测试与 production build 通过；嵌入资源已更新 |
| Swift / Go 构建 | App 与 helper 的 arm64 构建、Swift 类型检查通过 |
| CLI / DMG | 校验清单、解包、只读挂载、含空格的安装路径、App 完整性、CLI/App 版本一致通过 |
| App CLI 注册 | 真实 helper 安装、重复安装、执行、移除、未知文件保护通过；Go 回归另覆盖引号路径、参数原样传递、并发安装与路径更新 |
| Homebrew | 临时 Homebrew 前缀与 Applications 中 Formula、Cask 分别安装和升级通过；双向混装均拒绝，失败后回滚；Cask 命令链接指向 App 内 helper |
| 升级与 MCP | Formula 稳定 opt 路径、App 稳定 helper 路径通过；Formula 升级时已有兼容 Core 的 PID 与实例身份保持一致 |
| 生命周期 | 五路并发启动、规范化数据目录、默认端口回退、显式端口冲突、鉴权停止、MCP 延迟启动、崩溃恢复与卸载保留数据通过 |

Homebrew 测试发现并修复两项真实边界：冲突判断应检查成功安装收据，不能把失败安装遗留的目录当成已安装；Cask preflight 应抛出可回滚的异常，避免中途退出遗留安装记录。Formula 升级可能保留多个版本，切换渠道使用 `brew uninstall --formula --force teamcross` 移除所有已安装版本。安装脚本只处理临时前缀，没有改动用户的正式 Homebrew 或 `/usr/local/bin/teamcross`。

Formula/Cask 的升级验证使用相同二进制与变化的包版本，隔离检查包管理替换与稳定路径，不声称覆盖所有未来版本兼容性。Cask helper 保留下载隔离属性；本轮仅比较该 helper 的字节与已运行的本地副本，没有把比较结果当成首次系统批准已经通过。

## 真实运行时与页面

`TestLiveCodex` 在 `/private/tmp/teamcross-v011-live-host-20260911` 的专用仓库和 Codex home 通过：来源设置继承、原目录与 worktree 两个 fork 的实际文件执行、结束共享后独立 app-server 取得写入锁、只读历史不重新占锁、恢复同一会话 ID 和模型。初次在外层工具沙箱中运行时，测试目录写入触发额外审批；宿主环境重跑通过，产品权限设置没有为验收放宽。

`TestLiveNativeHandoff` 使用同一专用 fixture，不发送新 prompt；两种目录均通过 B 接入、A 接回和同一原生会话恢复，最终释放运行时。该结果验证同机真实协议和进程路径，不替代 TUI/Desktop 窗口或两台 Mac。

Chrome 的隔离测试会话检查首页、来源选择、加入、原目录详情、worktree 详情和设置，共 36 种页面、宽度与主题组合（1440/1024/768 CSS 像素，深浅主题），全局无水平溢出。实际查看代表性截图，并完成来源选择到目录确认的读取流程，没有额外创建协作或发送模型输入。设置页实际显示命令来源、版本、冲突提示与缺失客户端提示。浏览器控制台未记录 error/warning。

截图复核另发现长来源摘要导致列表内部横向滚动；收紧 Grid 列宽后，三种宽度与深浅主题的 6 项复验中，列表 `scrollWidth` 均等于 `clientWidth`，省略号正常显示。截图保存在本机 `output/playwright/v011-*`。

## 未覆盖范围与收尾

- 原生 UI 自动化选择 Team Cross App 返回 `timeoutReached`，本轮没有完成菜单点击、授权弹窗、退出确认和完整 Desktop 窗口矩阵；Swift 编译与 CLI 实测不替代这些操作。
- 两台真实 Mac 的 LAN、防火墙与睡眠恢复、其他 macOS 版本和 Intel 未验证。
- 下载后首次系统批准、Developer ID 签名与公证未验证；清单如实记录构建状态。公开 Release/tap 的可下载性和安装结果需要在实际发布后另行核对。

收尾按本次启动的 PID、父子关系和测试目录核对，关闭隔离 Chrome、测试 Core、原生 app-server 和安装辅助进程；不按应用名称批量终止用户实例。专用 fixture 与截图保留供复核。正式候选包必须从干净提交构建，并重新执行安装与 Homebrew 验证后才能用于发布。

相关入口：[分发设计](../distribution-and-onboarding.md) · [验证契约](test-gates.md) · [发布说明](../../../releases/v0.1.1.md) · [上一日邀请与释放验收](membership-and-release-2026-09-10.md)。
