# 分发与首次体验

本页维护安装后的入口归属、Core 与 App 生命周期、邀请打开方式及客户端就绪检查。构建和发布操作由 [构建与发布](releasing.md) 维护，产品动作见 [产品流程](product-flows.md)，实际验证范围见 [证据入口](validation/evidence-map.md#网络与分发)。

## 安装产物与边界

首版 Apple Silicon、macOS 14+。Swift/AppKit 菜单栏外壳复用 Go Core 和浏览器 WebGUI。CLI 包含 WebGUI 与 STDIO MCP，安装用户不需要 Go/Node/pnpm。Codex 按参与方式另行使用；B 查看上下文无需 Codex 或本地仓库。

安装产物包括独立 CLI、App 与 DMG，构建规则见 [构建与发布](releasing.md#构建产物)。App 的 helper 位于 `Contents/Resources/teamcross`。稳定版使用 `teamcross` / `team-cross`，RC 使用 `teamcross-rc` / `team-cross@rc`；Cask 安装 App 并用 `binary` 注册内置 CLI，Formula 提供独立 CLI。四种定义都会占用同一个 App 或 `teamcross` 命令，通过成功安装收据双向互斥；Core 仍按数据目录共享，包管理互斥不改变服务发现协议。

App 图标只使用 [AppIcon.png](../../../apps/macos/Assets/AppIcon.png) 生成 `TeamCross.icns` 的 10 个标准尺寸表示，`CFBundleIconFile` 保存无扩展名的资源基名。源图保持 sRGB、明确的不透明底板和透明外缘；构建在调用 `iconutil` 前移除临时 rendition 的扩展属性。WebGUI 图标与菜单栏状态图标是独立资源，不随 App 图标替换。

构建命令、发布顺序、GitHub / Homebrew 流水线及签名规则集中维护在 [构建与发布](releasing.md)。稳定版本号、Latest 或可安装不代表 Developer ID 签名与 Apple 公证，具体状态以对应构建清单为准。首版无登录自启和自动更新，升级由原安装渠道处理；卸载不清理协作数据和工作目录。

## 命令入口的归属

ChatGPT 插件通过 App 菜单“ChatGPT 插件…”或 WebGUI 设置页首次启用。用户点击安装后，App 后续启动会检查并同步已启用的插件，保留现有来源和 Core 目录。安装包复制到 Applications 本身不注册插件；断开后不自动重装。同步与界面实际加载分别显示，具体生命周期及 CLI 入口见[本机插件](decisions/chatgpt-local-plugin.md#包和生命周期)。

DMG 安装者在菜单栏“命令行工具…”安装或移除启动器，默认位置为 `/usr/local/bin/teamcross`。只有安装或移除该入口可能需要系统授权；Core、MCP 和协作执行不提权。启动器用绝对路径调用 App 内的 CLI，保持参数原样传递。相关代码为 [cliinstall](../../../internal/cliinstall/cliinstall.go) 与 [App 菜单](../../../apps/macos/TeamCross.swift)。

`cli-status` 检查命令路径、来源、入口归属和 PATH；`install-cli`、`uninstall-cli` 不启动 Core。高级用户可用 `--cli-dir /absolute/bin` 指定目录，测试必须使用隔离目录。Finder 不继承终端 PATH，因此另外检查常见 Homebrew 命令位置。已有 Cask 指向当前 App 的链接可直接使用，不再注册第二个入口；其他可执行文件、符号链接、被修改的启动器均报告冲突。

安装器仅更新完整匹配自身格式的启动器；通过目录中的生命周期锁串行化安装，新入口使用原子创建，升级使用原子替换。移除操作不触及 App、协作数据或 shell 配置。用户直接把 App 放进废纸篓不会执行清理脚本，卸载 App 前应从菜单移除手动安装的命令。

Formula 与 Cask 均检查稳定版和 RC 另一渠道的成功安装收据，验证覆盖同渠道两种安装顺序、跨渠道冲突及拒绝后的回滚。Cask 只移除 Homebrew 自己管理的 App 与命令链接；Formula 只移除自己的二进制。切换渠道前用户先退出服务，再用实际 token 卸载旧渠道；Formula 升级后可能保留旧版本，切换到 Cask 应使用 `brew uninstall --formula --force teamcross` 或 `brew uninstall --formula --force teamcross-rc` 移除相应 Formula。设置页与 `doctor` 分别显示已安装和正在运行的版本；升级不自动中断活动协作。

## 启动、发现与退出

菜单栏左键打开协作速览，`Control + Option + T` 注册成功时可全局呼出；“当前协作”和“资源速览”平级切换，可固定成浮窗，完整材料阅读在 WebGUI 打开。列表随页面统一滚动，筛选行的“服务与设置”与右键图标保留原有服务状态、设置、CLI 管理与退出入口。速览仍经公共启动器发现同一 Core，选择状态与浏览器共用；原生桥仅开放经过同源与路由校验的打开、固定和菜单动作。详见 [资源库决策](decisions/resource-library.md)。

[service](../../../internal/service/service.go) 是 CLI、MCP 和 App 共用的生命周期入口。`teamcross` / `serve` 默认后台启动并打开页面；`serve --foreground` 用于开发。`join` 自动确保服务，`mcp` 仅在实际工具调用时启动服务且不打开浏览器。App 通过同一 helper 的 JSON 命令操作服务，不维护另一套协作实现。

数据目录规范化（含符号链接），`start.lock` 串行化并发启动，`core.lock` 覆盖完整服务生命周期。`connection.json` 以 0600 原子写入实例身份、PID、URL、版本、控制协议、数据目录与本机控制 token。健康检查不启动 Codex，核对身份与协议后才复用；连接失效可启动新实例，不兼容或身份不匹配则报告处理入口。

菜单栏外壳在创建图标之前，由 [AppInstance](../../../apps/macos/AppInstance.swift) 取得同一规范化数据目录的 `app.lock`。它与 `core.lock` 独立；锁文件不删除，文件描述符不传给 helper。不同数据目录可以保留独立 App，路径别名不能绕过去重。

取得锁的 App 建立按本机用户与数据目录区分的 [CFMessagePort](https://developer.apple.com/documentation/CoreFoundation/CFMessagePort) 接收入口。后来的副本将首页或 `teamcross://join` 请求直接交给已有 App，不广播或落盘邀请。接收方对请求 ID 去重并回复入队确认；确认不表示已加入协作，邀请仍进入原有预览流程。客户端忙时排队处理，通信暂时不可用时有界重试；超时提示用户使用已有入口，不创建第二个图标，也不停止 Core。

副本转交完成和启动失败使用仅退出外壳的路径；只有用户对主 App 执行“退出 Team Cross”才进入原有停止服务流程。异常退出后系统释放外壳锁，下次启动可重新取得锁并复用存活的 Core。Finder 再次打开已有 App 时处理 reopen 事件，正常打开协作空间。旧版 App 不具备此协调协议，升级前仍需先退出旧外壳；新版本不按名称强制结束未知旧进程。

默认 43210 占用时回退到动态 loopback 端口；显式端口冲突不回退。控制 API 必须使用本机 token，拒绝浏览器 Origin；不凭陈旧 PID 或程序名终止进程。`status --json` 查询状态；`doctor --json` 检查客户端和 MCP；`stop` 对活动协作要求 `--force`，并等待生命周期锁释放。

浏览器和终端关闭不影响 Core。App“退出 Team Cross”停止本机服务，活动协作先确认；B 退出只断开 B，A 的运行时不随之停止。Core 停止保留 fork、目录、代码和加入记录；恢复与邀请失效继续遵循既有生命周期规则。不自动重放模型写入，也不自动恢复已撤销的共享。

## 首次协作

首次协作可先分享材料讨论，也可创建带执行的空间；来源选择、创建与邀请失败后的处理见 [产品流程](product-flows.md)，成员与执行权限见 [协作空间契约](decisions/collaboration-spaces.md)。本节维护邀请在本机的打开方式与客户端就绪检查。启动不要求 `--repo`；此高级参数只设置本机辅助上下文。

邀请同时给出 `teamcross://join?invite=…`、原始 `tcx3.` 内容及安装指引。App 将邀请经带本机凭据的接口暂存，浏览器只携带随机 pending ID。暂存最多 32 项、10 分钟，仍受原邀请期限约束。邀请声明的名称、主机和 `lan|tailcat` 在预览显示，实际加入使用相同 TLS pin 验证；预览只做本地格式和版本校验，不联系远端、不启动模型。Tailcat 邀请还包含其地址及预共享密钥，安装指引和页面明确提醒用户把整份邀请码视为秘密。

加入后进入所获授权的空间上下文；页面关闭不停止本机 Core 的在线心跳。多人参与、成员资格与执行访问按 [协作空间契约](decisions/collaboration-spaces.md) 判断，申请和交接按 [输入协调](decisions/input-and-sharing.md) 处理；心跳周期、在线窗口与请求字段统一维护在 [共享协议](protocol.md#共享邀请与传输)。

直接客户端以连接建立、成功读取或恢复对应 thread 区分 `connected` 和 `session_ready`。Desktop 仍需手动打开会话时如实说明。配置发现支持显式 CLI、安装 App 内的 CLI、PATH 与系统/用户 Applications；实际版本可诊断，实验 Desktop 接口不视为公开稳定合同。

MCP 配置保存稳定 opt/App 绝对路径。通过 Cask 命令链接调用时也解析回 App helper；不保存版本化 Cellar 路径或依赖 shell alias。个人 Codex 或 Claude Code 分别配置；Claude 使用 user 范围原生配置命令，遇到当前项目同名覆盖或禁用时提示处理。配置存在、独立 STDIO 协议探测和各 Provider 实际客户端工具调用分别记录；仅配置成功不代表旧客户端已重载工具。协议探测不启动模型，也不记录为实际客户端调用。

## 检查与相关规范

[安装验证](../../../scripts/verify-release.py) 验证校验文件、DMG 挂载/安装、CLI/App 版本一致与实例复用，并调用 [App 副本验证](../../../scripts/verify-app-instance.py)。后者从两个临时 App 副本通过真实 macOS 启动/URL/退出事件验证外壳去重、请求确认、卡住后的恢复及 Core 保留；邀请 helper 使用测试内容，Core 使用包内真实二进制，不打开浏览器或调用模型。[生命周期验证](../../../scripts/verify-lifecycle.py) 验证 Core 并发启动、符号链接路径、端口冲突、鉴权停止、MCP 延迟启动、崩溃恢复及数据保留。工程、真实客户端、浏览器和清理门槛继续按 [验证契约](validation/test-gates.md)。

证据只对应各记录中的提交、runner 与主机，后续基础设施失败不能静默跳过或沿用旧结论。同机或托管 runner 不能证明真实 macOS 26 图标显示、两台 Mac LAN、跨网络 Tailcat 或强制 DERP，未使用 Developer ID 的产物不能证明公证或 Gatekeeper 首次批准通过。

相关来源：[产品流程](product-flows.md) · [架构](architecture.md) · [协议](protocol.md) · [输入协调](decisions/input-and-sharing.md)。

具体版本的实际结果与未覆盖项按 [任务记录规则](../README.md#任务记录) 保存，影响当前判断的摘要与指针维护在 [证据入口](validation/evidence-map.md#网络与分发)；本页不累积逐版验收清单。历史开发包的共存设计不作为当前分发规则。

外部规范：[Apple 自定义 URL Scheme](https://developer.apple.com/documentation/xcode/defining-a-custom-url-scheme-for-your-app) · [Homebrew Cask Cookbook](https://docs.brew.sh/Cask-Cookbook)。
