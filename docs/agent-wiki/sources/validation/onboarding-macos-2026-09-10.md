# 首次体验与 macOS 安装验收

验证日期：2026-09-10。对象为 `389608e7b548` 基线上的本轮未提交工作区，产物版本 `0.2.0-dev`，控制协议 `1`。本页记录本轮实际检查，不沿用上一日的 Desktop 验收结论。

## 环境与产物

Apple Silicon Mac，macOS 14.8.5，Go 1.25.3，Node 24.18，Swift 6.0.3，Command Line Tools SDK 15.2，Homebrew 6.0.22，Codex CLI 0.153.1。App 最低部署版本为 macOS 14，未验证 Intel 与其他 macOS 版本。

`scripts/build-release.py` 在 `dist/release/` 生成 CLI tar.gz、`Team Cross.app`、DMG、`SHA256SUMS`、`release.json` 和 `homebrew-teamcross/`。所有产物来自同一份版本与提交信息，清单明确记录 dirty 状态。当前只有 ad-hoc 签名，没有 Developer ID 签名、公证或公开发布。

## 工程与安装

| 检查 | 本轮结果 |
| --- | --- |
| Go test / vet | 通过；包括邀请解析、pending ID、输入申请、在线心跳、服务发现和 MCP 回归 |
| Go race | `collab`、`mcp`、`sharing`、`nativecodex`、`service` 通过 |
| Web check / test / build | 通过，12 项交互测试；嵌入资源已更新 |
| Swift 与 Go 构建 | arm64 构建通过；App/helper 的 ad-hoc 完整性检查通过 |
| CLI 与 DMG 安装 | `verify-release.py` 通过；校验值、解包、只读挂载、带空格应用路径、版本一致与实例复用通过 |
| 本地 Formula / Cask | `verify-homebrew.py` 在临时 Homebrew 前缀和临时 Applications 中通过安装、共存与卸载；没有修改正式 Homebrew 前缀 |
| 稳定 MCP 路径 | 安装测试发现 bin 符号链接未统一为 opt 的问题，修复并增加回归；实际安装后检查通过 |
| 版本替换 | 临时 Formula 版本升级后，CLI 复用原 PID 与实例身份，稳定 opt 路径保留。该用例以相同二进制隔离验证包管理替换；协议不兼容由 Go 测试另行覆盖 |
| 卸载保留数据 | 临时安装卸载后，测试数据标记保留；没有删除协作仓库、worktree 或用户原有会话 |

Homebrew Cask 下载的未签名包带隔离属性，首次执行受到系统检查。安装验证不清除该属性，也不代替用户批准首次启动；Cask 验证比较 App/helper 字节，非隔离的本地 DMG 副本单独完成 helper 运行检查。不能据此宣称公开下载后免除 Gatekeeper 操作。

## 生命周期与首次使用

- 生命周期脚本通过五路并发启动、重复打开、规范化/符号链接数据目录、默认端口回退、显式端口冲突、无凭据停止拒绝、陈旧连接文件、崩溃后重新启动及数据保留。
- STDIO initialize/tools/list 不启动 Core；首次实际工具调用启动 Core 且不弹浏览器。兼容 Core 可复用，不根据旧 PID 杀进程。
- 有活动协作时 CLI `stop` 被拒绝，必须明确使用 `--force`；CLI 错误同时显示下一步操作。关闭 B 的服务后，A 的原生运行时仍在线。
- 浏览器实际完成来源选择、原目录确认、“创建并邀请”、邀请预览、B 加入、历史读取、输入申请/取消和 A 明确交接。B 测试 Core 指定不存在的 Codex 路径，仍能加入并读取上下文。
- 分享失败后保留已 fork 的协作、只重试邀请，由 Web 回归测试覆盖；没有自动重放写入。
- 本地菜单栏 App 实际启动/复用服务并打开 WebGUI；实际接收 `teamcross://join`，把邀请交给 Core，打开只带随机 pending ID 的预览页。确认前没有自动加入，确认后显示共享上下文。

## 原生与界面证据

`TestLiveCodex` 在专用目录运行通过。来源与两个 fork 均使用 `gpt-5.6-luna`，验证模型设置继承、原目录保留未提交内容、新 worktree 从 HEAD 创建、两个执行目录中的证明文件和显式恢复。

| 入口 | 原目录 | 新 worktree |
| --- | --- | --- |
| 直接 TUI | 本轮实际恢复 fork、显示历史、使用工具读到 `unstaged` baseline 和证明文件 | 本轮实际恢复同一 worktree fork、读到 `committed` baseline 和证明文件 |
| 辅助 TUI | 普通独立 Luna 会话通过 MCP `get_collaboration`/`read_context` 读到 A 原目录和 `unstaged` baseline | 同一普通会话通过 MCP 读到 worktree 执行目录和 `committed` baseline |
| 直接 Desktop | 本轮未完成原生 UI 验收 | 本轮未完成原生 UI 验收 |
| 辅助 Desktop | 本轮未完成原生 UI 验收 | 本轮未完成原生 UI 验收 |

TUI 建立连接之后，只有成功恢复对应原生会话才观察到 `clientState=session_ready`。退出 TUI 后 Core 继续运行。普通 Codex Desktop 保持独立，无需退出才能由另一人使用专用客户端。

辅助 TUI 使用独立测试仓库与测试 Codex 配置；MCP 配置没有写入用户普通 Codex home。此轮只通过工具读取两个目标，没有向共享会话发送输入。真实 STDIO 工具调用和返回内容已观察到，未将“配置成功”当作“客户端已经加载”。

最终浏览器检查覆盖首页、来源选择、目录确认、详情、加入、设置和邀请弹窗：各自在 1440、1024、768 CSS 像素及深浅主题下生成截图，共 42 张，均无水平溢出。另有客户端缺失、无效邀请与停止 Core 后的断线状态截图，已实际查看代表性页面、窄窗口、长路径和失败状态。断线显示中文恢复指引，写入结果不明时不自动重放。截图保存在本机 `output/playwright/`。Escape 关闭弹窗并恢复触发按钮焦点，取消输入申请与交接通过实际键盘/按钮操作。

本机原生 App/UI 自动化读取连续超时，无法可靠检查菜单点击、退出确认弹窗及 Desktop 操作。菜单栏 App 的服务启动与邀请 URL 已通过 CLI/浏览器侧观察；这不代表原生菜单交互或全部八种客户端组合已通过。本轮没有以后台 app-server 测试替代 Desktop 验收。

## 尚未完成的验收

- 两台真实 Mac 的 LAN、系统防火墙/休眠恢复；同机 loopback/TLS 结果不替代这些检查。
- 菜单栏退出确认、异常退出后的原生菜单显示、邀请冷启动/重复点击的完整 GUI 矩阵，以及两种目录下直接/辅助 Desktop 的真实操作。
- 带隔离属性的未签名 App 首次系统批准、Developer ID 签名/公证、其他系统版本、Intel 和公开 Homebrew/Release 安装。

## 收尾与保留

仅关闭本次测试启动的 TUI、App、Core、app-server、浏览器测试标签页和安装辅助进程。测试 App 增加外部限时守护，界面读取超时后也停止其隔离 Core。没有按 Codex、Chrome 或 Edge 名称批量杀进程。

专用测试根目录为 `/private/tmp/teamcross-onboarding-20260910`，保存测试仓库、会话和结果材料以便复核；不保存真实邀请或登录材料到 Wiki。原有 README 产品方向、原生数据目录、用户会话及工作目录均保留。

相关入口：[分发与首次体验](../distribution-and-onboarding.md) · [验证契约](test-gates.md) · [上一轮原生客户端验收](native-collaboration-2026-09-09.md)。
