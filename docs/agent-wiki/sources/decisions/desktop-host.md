# Wails 桌面外壳

用户已确认采用 Wails v3 + 现有 React + 独立 Go Core，在 `Next` 上按关键功能通过 PR 逐步集成。迁移进度与阶段边界见 [迁移任务](../../tasks/desktop-wails-migration.md)，本页维护长期架构边界。

## 职责

- `apps/desktop` 是独立 Go module，锁定 `v3.0.0-beta.26`。原生 UI 依赖不进入根 Core module，嵌套 module 的测试必须显式执行。
- 外壳负责窗口、原生桌面能力与本机服务连接；业务执行、会话与输入归属仍在独立 Core。不得在窗口进程实例化 `collab.App` 或第二套 Provider 运行时。
- 内嵌资源通过 Wails 进程内资源传输加载，外壳不增加 TCP listener。React 主窗口接入后，浏览器继续共享组件与 Core API。
- Core 的长效控制 token 留在原生内存。页面只有每次外壳启动生成的资源请求能力值；它不是 Core 凭据。原生入口先验证来源与请求，再按路径/方法白名单转发，不给普通页面 API 自动加控制 token。
- 发现文件必须是私有、本机、显式端口，原生探测核对实例、PID、数据目录、版本、提交与控制协议。预览先要求同次构建；未来允许跨版本时必须增加明确兼容契约。
- 不通过系统 HTTP 代理发送请求，不跟随重定向，不自动重放写入。失联和恢复更新连接状态，不替用户再次提交业务动作。

## 当前实现范围

当前 [预览入口](../../../../apps/desktop/main.go) 显示与浏览器共用的完整 React 页面，要求显式隔离 `--data-dir` 或 `TEAMCROSS_DATA_DIR`，拒绝默认生产数据目录。只有持锁实例通过内置 CLI helper 确保该目录的独立 Core；`--connect-only` 用于不允许启动的 fixture。显式退出按下述规则停止对应实例；不注册生产邀请协议，尚未替换 Swift App 或正式安装包。`--probe` 保留最小请求通道诊断页。

[platform.ts](../../../../packages/web/src/platform.ts) 集中处理首次语言读取、业务请求、复制和窗口操作。Core 路由按方法与路径列明，控制及运行时专用入口继续不可达。剪贴板与窗口操作只提供固定的 `/desktop/clipboard`、`/desktop/open`、`/desktop/pin`、`/desktop/menu` POST，使用同一资源能力验证；打开路由限制为已知页面、材料 key 与协作 ID，拒绝任意 URL、脚本或邀请参数。外链仅允许内嵌主页面中主动点击的 HTTP(S) 导航，交给系统浏览器，不新建 WebView 或加载远程页面。

主窗口和速览各保留一个 React 页面。菜单栏左键、窗口菜单与已注册的全局 `Control + Option + T` 开关速览；右键和页面的服务入口在速览内显示原生菜单。未固定速览失焦收起，固定后使用浮窗层级，关闭只隐藏；材料和协作定位只改变已有主窗口的 hash 路由。原生菜单每 5 秒读取轻量状态，语言使用 Core 确认的同一本机偏好；语言保存只有一次公共 API 写入，不带控制凭据，不自动重放。

共享 React 不引入 Wails JavaScript runtime 或业务 bindings，因此不依赖该 runtime 的 ready 事件执行页面通知。原生适配只向已安装导航 guard 的 WKWebView 发送固定的定位、固定状态和刷新事件；页面未完成加载时使用有界待发队列。固定或隐藏不重建 WebView，普通重新显示主窗口不改变路由或草稿。

主题由 Core 的 `settings.json.uiTheme` 保存，通过 `/api/ui-theme` 在主窗口、速览与 WebGUI 之间共享，值为 `system`、`light` 或 `dark`，默认跟随系统。页面只在写入得到确认后应用选择；后续可见性读取确认其他窗口的更改。原生标题栏与窗口控件读取同一已保存偏好。主题的首次/离线画面缓存、客户端选择与短期创建状态使用数据目录哈希前缀隔离，浏览器缓存键名保持原状；缓存不自动导入 Core。连接失败保留已有 React 树、路由与未提交草稿，后续轮询或显式刷新恢复读取，业务写入仍需用户提交。保存失败继续显示上次确认的外观并提示错误；不自动重放主题写入。

页面资源、资源库与语言的后台读取统一通过 [可见性调度](../../../../packages/web/src/visibility.ts) 执行。浏览器使用 DOM visibility；Wails 在导航 guard 中补充 AppKit 窗口隐藏、最小化、完全遮挡和 App 隐藏状态。失去焦点本身不暂停。不可见时取消 GET 与定时器，恢复可见时立即读取，同一读取未完成前不再启动一份。页面与草稿不卸载，业务 POST 不接入这一读取调度；菜单轻量状态检查和独立 Core 继续运行。显式打开、Dock/reopen 与快捷键恢复主窗口时同时解除最小化。

关闭窗口只隐藏，Dock 或菜单重新打开保留页面。显式退出沿用“停止本机服务并退出”契约：读取活动协作，默认取消；确认后再次验证同一 Core 实例并发送一次 stop，等待 Core 锁释放后才结束外壳。取消、超时、实例变化或响应丢失保留窗口，不自动重发 stop。无活动协作使用非强制停止，让 Core 拒绝竞态中新启动的协作；正在启动、失联但进程仍在运行或身份不兼容的 Core 不被当作已停止。第二个外壳转交后退出和启动失败不会停止 Core。`--leave-core-running` 只用于不由外壳停止的测试 fixture。

Core 控制接口仅由原生退出协调器调用，普通 renderer API 仍无法访问；对外状态不带 token。退出确认使用 Core 返回的共享语言偏好，失联错误读取 Core 保存的本机偏好。处理退出期间拒绝新的转交回执，发送者沿用原请求重试；UI 取消不重建页面或清除草稿。

[appinstance](../../../../apps/desktop/internal/appinstance/) 保留 Swift 的 `app.lock` 与 CFMessagePort 身份算法（用户 ID + 规范化数据目录）、版本 1 请求及入队回执。启动事件循环可初始化隐藏窗口，但持锁前不显示主窗口、创建菜单栏或注册全局快捷键，也不启动 Core；第二份外壳带固定请求 ID 有界重试，收据成功后只退出自身。锁文件保留，崩溃由系统释放锁与 IPC；独立数据目录使用独立身份。回调仅入队，原生 UI 在队列外处理，避免等待 AppKit 弹窗才返回回执。

通信名称必须使用与 Swift 相同的 Foundation 路径表示；例如 Foundation 的 `/tmp` 与 Go 的 `/private/tmp` 可指向同一目录，却产生不同哈希。锁和 Core 发现仍按 `service.Normalize` 的规范化路径处理。双向旧 Swift 互通由真实实现回归覆盖。

显示窗口与邀请转交使用有界内存队列；最多 32 项邀请/批次，生命周期 10 分钟。Preview 的测试 scheme 在原生端归一为既有 `teamcross://join` IPC 契约，不注册生产 scheme。私有邀请暂存方法经过 Core 身份探测，只返回经过格式检查的 pending ID；普通 React API 仍无法访问 private staging 路由。邀请确认使用独立窗口，主窗口保留路由和草稿；Core 重启后暂存失效，不重新入队或自动加入。实现与实际 Apple Event 验证是不同证据，具体检查状态保存在迁移任务；同机副本回归也不能推广为正式安装切换验收。

构建与隔离 fixture 使用方法见 [桌面 README](../../../../apps/desktop/README.md)。基础模块和网络边界测试不能代替真实 WKWebView、菜单栏、邀请、原生客户端和安装验收。生产范围继续为 Apple Silicon/macOS 14+。
