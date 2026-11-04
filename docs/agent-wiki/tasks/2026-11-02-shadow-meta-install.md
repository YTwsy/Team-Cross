# 分身收敛分支合入 Meta 与本机安装

日期：2026-11-02。用户授权在完成消息流收敛后，将 Meta 合入当前 `codex/shadow-activity`，构建并安装新版本。

## 来源与合并

- 分身实现独立提交：`bd6f7fbf4296aa70a1706a12de996d364c249ccd`。
- 合入本地 `Meta`：`956a13d7b8b545c240bf0310ed76273055c79730`。当时远端 Meta 为 `3cb3afa`，本地包含后续阅读体验与截图管理修改；未改动 Meta 本身或推送远端。
- 合并及安装包来源：`2f237150f35873a3a6d16eb4a632840303892cd2`。包从该提交的干净工作树生成，清单 `dirty:false`。
- 产品源码没有冲突。两份历史设计文档保留分身分支中的实施记录链接；前端散列文件和 panel.html 从合并后的源码重新生成，不手工拼接产物。
- Meta 移出 Git 的 87 张验收截图已经保留为本地忽略文件，没有删除用户截图或重新纳入提交。

本次是在[消息流实施记录](2026-11-02-shadow-message-input-implementation.md)之后的独立验证，不将合并前结果直接视作合并后的通过结果。

## 安装产物

| 项目 | 结果 |
| --- | --- |
| 版本 | `0.2.6-dev.shadow-activity.20261102` |
| buildNumber | `210` |
| 架构 / 系统下限 | arm64 / macOS 14.0 |
| 安装位置 | `/Applications/Team Cross.app` |
| CLI 入口 | `/opt/homebrew/bin/teamcross`，继续指向 App 内 helper |
| 运行构建 | App 内 CLI、终端入口及运行中 Core 均为 `2f23715` / 上述版本；菜单栏 App 已重启 |
| 签名 | ad-hoc；未使用 Developer ID，未进行 Apple 公证 |
| DMG SHA-256 | `54ffa8a7392ea2f295bc0b7b87a5e61a28c9639559342b45dcd220daaf9b166d` |
| CLI tar.gz SHA-256 | `eda1f0c45a9f65d982407f82524b99b4409ef9f637949cb44768585603c790cd` |

产物在本工作树 `dist/release/0.2.6-dev.shadow-activity.20261102/`，包含 App、DMG、CLI tar.gz、SHA256SUMS 与 release.json。该目录仅本地保存，没有创建 tag、GitHub Release 或发布 Homebrew 配方。

旧安装为 `0.2.6-dev.member-activity.20261023` / `c7612db`。替换前已经核对并备份，原 App 保留在本工作树 `.local-backups/install/2026-11-02-shadow-meta/original-installed.app`，另有校验前复制的 `Team Cross.app`。没有删除协作数据、用户工作目录或个人 Codex 会话。

安装前唯一在线协作处于等待输入、无审批且没有连接中的客户端。安装沿用现有有界 Core 升级协议；该执行及原生 session ID 保留，Core 重启后为暂停状态，没有自动重放任何输入。菜单栏 App 的旧进程按准确 PID 和路径核对后正常退出，新进程复用已升级 Core。没有按名称批量终止其他程序。

## 实际检查

- `go test ./...`、`go vet ./...` 通过。
- shadow、collab、mcp、nativecodex、service、cliinstall、mcpassets、materialstore、pluginpack 的 race 通过。
- Node 24.18.0：Web check 1366 条翻译、29 个文件 257 项测试、两套 production build 通过；提交后 release 再次构建 Web 没有产生差异。测试数变化来自合入 Meta 后移除正文搜索界面的既有测试，并非跳过分身测试。
- 隔离阅读器真实浏览器复核：中文浅色下表格换行、图集切换与缩放复位；英文深色阅读器和分身入口；实际 CSS 宽度 1440/1024/768；无横向整页溢出、无正文搜索入口和浏览器错误日志。原生历史为合成，不代表真实 Provider 图片验收。
- `make release VERSION=0.2.6-dev.shadow-activity.20261102` 与 `make verify-release VERSION=0.2.6-dev.shadow-activity.20261102` 通过。覆盖校验值、DMG 挂载安装、图标反提取、签名完整性、CLI/App 版本一致、命令入口归属、Core 生命周期及两个真实 App 副本的启动/退出/转交。
- 使用**最终 App 内 CLI**运行 `scripts/verify-local-shadow.py --message-inputs`：同一 Mac 三个 Core、真实 TLS 成员传输、`gpt-5.6-luna`；三位成员的消息以三个批次进入同一 turn，作者与回复目标准确，W 慢工具释放前 S 已回答新问题，进展与完成直接送达，无 S 结果改写轮。离线手动重试、准确 thread 和记忆恢复及真实 stdio MCP 读取也通过。
- 安装后实测 App helper、原 CLI 链接、运行 Core 的版本/提交一致；菜单栏 App 为新的唯一进程。

新验收截图在 `output/acceptance/2026-11-02-shadow-meta-install/screenshots/`，仅本地保存并受忽略规则覆盖。原始日志位于 `/private/tmp/teamcross-shadow-meta-install-20261102/`。长期保存的[精简证据](2026-11-02-shadow-meta-install-evidence.json)包含原始日志散列、构建清单、来源及验证结果，不包含凭据、真实邀请或原协作的私密正文。

本次测试 Core、原生子进程、阅读器夹具及测试浏览器页已关闭；正式安装的 App 和 Core 保持运行。没有双 Mac、完整真实模型的默认 @ → Work → 原个人会话闭环验收；完整 ChatGPT 插件宿主和 Apple Gatekeeper 首次批准也不由这些结果推导。
