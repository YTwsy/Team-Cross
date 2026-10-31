# 插件更新后的重复刷新与两分钟回退

> 截图仅本地保存，路径相对本文，不随 Git 分发；保存与引用方式见[验收截图约定](../../README.md#验收截图)。

日期：2026-10-23。产品修复：`e99cd6742a59d884057b4e07c4e575114efb6721`，基于安装中的 `73e503c94b9a65addaac39c6d765e6556eb1f0c3`。本机 macOS arm64，ChatGPT `26.1016.61520` / build `21421`。采用独立 `codex/plugin-hot-update-fix` worktree，未混入主检出目录中并发开发的原生会话工作区改动。

## 复现与根因

用户顺序：退出旧 Team Cross，保留 ChatGPT；更新 App 后先打开插件，再启动新版菜单栏 App。

旧 stdio 进程仍返回其内嵌旧 HTML，但会从安装中的新版 Core 读取 bundle。旧 `replacePanel` 等待插入的内联 module 触发 `load`；Chromium 中代码已经执行、React 已挂载，该事件却不会自动触发。120 秒计时器随后把正常的新界面判为失败，恢复旧界面，并显示“刷新未完成，草稿已保留，请重试”。这解释了短暂看到新版后又刷新回旧版的现象，不能以初次显示成功作为验收。

修复前先增加真实 `replacePanel` 回归并确认失败；浏览器以 production 资源、真实 Core 和 `connect-src 'none'` 重现：新 revision `1b0e4e9d11e2…` 在超时后恢复为 `6c3838d01c23…`，错误横幅出现。独立内联 module 探针也确认异步挂载完成而 `load` 未触发。

## 修复

- 新入口在宿主初始化和 React 首次提交后发布 runtime 与明确就绪事件，同时发出已打开旧加载器需要的兼容 `load`。只修改新加载器不足以修复已经运行的旧面板。
- 加载器核对 runtime 实际变更；只接受当前脚本的启动错误，忽略无关宿主错误。超时回退前取消未完成的桥握手，迟到回执不会覆盖恢复后的编辑器。
- 所有更新失败阶段均保留失败 revision，跨重挂载不自动重试同一版本；错误诊断带阶段和原因，不含草稿正文。成功更新清除旧候选版本。
- 同步重建普通 WebGUI 和 MCP 面板嵌入资源。

## 工程与隔离验收

- Go 全量 test/vet，以及 `internal/mcp`、`internal/service`、`internal/pluginpack`、`internal/collab` race 通过。
- Web check（含 1157 条消息完整性）、23 个测试文件 / 218 项测试、两种 production build 通过。Vite 仍提示现有大 chunk，不影响构建。
- 浏览器以实际旧面板 `ee436d1648c7…` 启动，升级到含修复的 `9185d39373ac…`，132 秒后仍为新版、无可见错误横幅、只有一个 module 标签。随后升级第二版：草稿阻止自动更新，点击后正文完整恢复。第三版故意抛出异常：恢复第二版和完整草稿，记录 `startup / fixture startup failure`，不自动循环；全程业务写入为零。
- `verify-plugin-update-order.py` 保留同一个旧 stdio PID，停止旧 Core、替换安装、先从旧 MCP bootstrap 启动新 Core，再执行 App 的 serve/export 命令。新 Core PID 保持不变，bundle SHA-256 一致，旧 MCP 始终存活。脚本退出关闭自建 Core/stdio；无模型调用或个人配置变更。

## 实际桌面验证

安装前 Core 活动协作为零。保留原 App 备份，安装本地开发版 `0.2.6-dev.plugin-hot-update.20261023`，来源为上述干净产品提交。ChatGPT PID `34424` 和后台 PID `34928` 始终未变，启动时间仍分别为前一天 20:34:36 与 20:34:42。

宿主禁止自动操作 ChatGPT 主窗口，因此未关闭再重新创建该面板，而是保留实际旧面板与旧 MCP：停止旧 Core 时，打开的面板会立即将其重新拉起，使 CLI 的停止等待报告失败。确认无活动协作后，在受认证停止请求之后立即替换 App，避免把该现象误判为修复失败。菜单栏 App 尚未启动时，真实旧面板已切换到 `ae67085a17e12f1ab4061a433090aaa0d6c95b0b462b5373f923b88adff33a39`，新版 Core PID `8066` 由 13:45:20 已启动的旧 MCP PID `96816` 拉起。

随后经桌面启动已安装 App：界面控制接口超时，但进程核对证明新版菜单栏 PID `9435` 于 14:53:42 启动，仍复用 Core PID `8066`。插件可以正常导航到空白的加入协作表单，无业务写入。从首次观察到新版起 166 秒后，revision 仍为上述新值、无可见更新横幅、module 标签为 1；空白邀请表单可正常显示和导航。

完整用户顺序由隔离的真实可执行文件验证；实际桌面覆盖已经打开的旧面板、宿主不重启、先插件再菜单栏 App 的切换。没有新增其他桌面版本、两台 Mac、真实模型中断或 Cloud 验收。对尚未具备 bundle 更新机制的更早插件，不声称磁盘替换能修改其运行时代码。

## 成品

本地开发版通过 DMG 只读挂载、SHA-256、App 签名完整性和 helper 版本/commit 对照；仅 ad-hoc 签名，未进行 Developer ID 签名或公证。没有发布远端 Release、推送分支或修改其他开发分支。

## 截图证据

- 修复前两分钟回退（`before-timeout.png`）
- 旧加载器升级后 132 秒（`after-132s.png`）
- 故意启动失败后保留草稿（`failure-draft.png`）
- 实际宿主升级后 166 秒（`live-after-166s.png`）
