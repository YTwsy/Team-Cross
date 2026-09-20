export type Mode = "existing" | "worktree";
export type RuntimeMode = "restricted" | "trusted";
export const runtimeModeName = (mode?: RuntimeMode) =>
  mode === "trusted" ? "信任模式" : "受限模式";
export const runtimeModeDescription = (mode?: RuntimeMode) =>
  mode === "trusted"
    ? "沿用邀请者的原生配置与权限，包括 MCP、插件、hooks、网络、命令及已启用的浏览器和电脑控制。操作可能访问工作目录之外的数据，并使用邀请者的服务授权。"
    : "使用 Team Cross 的受限运行配置，关闭个人 MCP、插件、hooks 等扩展，按协作权限处理代码操作。";
export type ShareTransport = "lan" | "tailcat";
export type AnnotationTarget = {
  kind: "history" | "file" | "changes" | "material";
  materialId?: string;
  version?: number;
  sessionId?: string;
  path?: string;
  startLine?: number;
  endLine?: number;
  side?: "old" | "new";
  turnId?: string;
  itemId?: string;
  startOffset?: number;
  endOffset?: number;
  cursor?: string;
  quote: string;
  contentHash?: string;
  baseRevision?: string;
};
export type Annotation = {
  materials?: MaterialReference[];
  authorId?: string;
  id: string;
  text: string;
  reference?: string;
  target?: AnnotationTarget;
  author: string;
  createdAt: string;
  replies?: AnnotationReply[];
};
export type AnnotationReply = {
  materials?: MaterialReference[];
  authorId?: string;
  id: string;
  requestId: string;
  text: string;
  author: string;
  createdAt: string;
};
export type Collaboration = {
  hasExecution?: boolean;
  executionAvailable?: boolean;
  invitationId?: string;
  invitationReadOnly?: boolean;
  reachable?: boolean;
  materials?: Material[];
  runtimeMode?: RuntimeMode;
  provider?: Provider;
  nativeWaiting?: string;
  capabilities?: {
    nativeTui: boolean;
    nativeDesktop: boolean;
    sendInput: boolean;
    steerInput: boolean;
    interruptTurn: boolean;
    respondToRequest: boolean;
  };
  id: string;
  title: string;
  sourceId: string;
  sessionId: string;
  workspaceMode: Mode;
  executionCwd: string;
  repo: string;
  head: string;
  branch: string;
  host: string;
  role: "owner" | "remote";
  writer: string;
  selfId?: string;
  members?: {
    id: string;
    name: string;
    active: boolean;
    online: boolean;
    inputRequested: boolean;
    joinedAt: string;
    executionAccess?: boolean;
  }[];
  invitations?: {
    id: string;
    state: string;
    expiresAt?: string;
    joinedCount: number;
  }[];
  state: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
  online: boolean;
  busy: boolean;
  sharing: boolean;
  sharingPreparing?: boolean;
  transport?: ShareTransport;
  connected: boolean;
  participantOnline?: boolean;
  participantJoined?: boolean;
  invitationState?: "active" | "expired" | "revoked";
  runtimeState?: "running" | "starting" | "releasing" | "released" | "offline";
  releasePending?: boolean;
  inputRequested?: boolean;
  clientState?: "disconnected" | "connected" | "session_ready";
  client?: string;
  approvals: number;
  epoch: number;
  sequence: number;
  invitation?: string;
  expiresAt?: string;
  annotations: Annotation[];
  model?: string;
  modelProvider?: string;
  reasoningEffort?: string | null;
};
export type Source = {
  provider?: Provider;
  id: string;
  name?: string;
  preview: string;
  cwd: string;
  updatedAt: number;
  status?: { type: string };
};
export type Preview = {
  runtimeMode?: RuntimeMode;
  source: Source;
  sourceTurnId: string;
  previewHash: string;
  workspace: {
    workspaceMode: Mode;
    repo: string;
    sourceCwd: string;
    executionCwd: string;
    head: string;
    branch: string;
    dirty: boolean;
  };
  targetDirectory?: string;
};
export type Provider = "codex" | "claude";
export type MCPClientStatus = {
  configured: boolean;
  command: string;
  configError?: string;
  observedAt?: string;
};
export type Info = {
  mcpClients?: Record<Provider, MCPClientStatus>;
  claudeBinary?: string;
  claudeVersion?: string;
  claudeError?: string;
  host: string;
  version: string;
  installedVersion?: string;
  cli?: {
    executable: string;
    command?: string;
    source: "unavailable" | "app" | "formula" | "standalone";
    conflict?: string;
  };
  binary: string;
  codexVersion: string;
  codexError: string;
  desktopApp: string;
  dataDir: string;
  mcpCommand: string;
  mcpConfigured: boolean;
  mcpProbed?: boolean;
  mcpObservedAt?: string;
};
export type ClientPlan = { command: string; launched: boolean; note?: string };
export type History = {
  contentHash: string;
  pageCursor: string;
  nextCursor: string;
  scope: "stream" | "item";
  itemComplete?: boolean;
  sourcePageComplete?: boolean;
  pageEndsAtTurnBoundary: boolean;
  thread: {
    id?: string;
    name?: string;
    model?: string;
  };
  turns?: { id: string; label: string }[];
  segments: MaterialPage["segments"];
};
export type Changes = {
  stat: string;
  diff: string;
  status: string;
  contentHash?: string;
  baseRevision?: string;
  truncated?: boolean;
};
export type FileContext = { path?: string; text: string; contentHash?: string };
export const projectName = (path = "") =>
  path.split("/").filter(Boolean).at(-1) || "项目";
export const sourceName = (s: Source) =>
  s.name || s.preview?.slice(0, 72) || "未命名会话";
export function relativeTime(date: string | number) {
  const diff = Math.max(
    0,
    Date.now() -
      (typeof date === "number" ? date * 1000 : new Date(date).getTime()),
  );
  if (diff < 60000) return "刚刚";
  if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`;
  return new Date(
    typeof date === "number" ? date * 1000 : date,
  ).toLocaleDateString("zh-CN", { month: "short", day: "numeric" });
}
export function status(c: Collaboration) {
  if (c.state === "expired") return { text: "邀请已到期", tone: "warning" };
  if (c.state === "ended") return { text: "共享已结束", tone: "muted" };
  if (c.state === "left") return { text: "已离开", tone: "muted" };
  if (c.state === "error") return { text: "需要处理", tone: "warning" };
  if (c.state === "preparing") return { text: "创建中", tone: "blue" };
  if (c.state === "joining") return { text: "等待确认加入", tone: "warning" };
  if (c.hasExecution === false)
    return {
      text: c.reachable === false ? "暂时断线" : "只读分享与讨论",
      tone: c.reachable === false ? "muted" : "green",
    };
  if (c.runtimeState === "releasing")
    return { text: "正在释放会话", tone: "muted" };
  if (c.runtimeState === "released")
    return { text: "会话已释放", tone: "muted" };
  if (!c.online) return { text: "等待连接", tone: "muted" };
  if (c.nativeWaiting) return { text: "等待原生交互", tone: "warning" };
  if (c.approvals > 0) return { text: "等待审批", tone: "warning" };
  if (c.busy) return { text: "运行中", tone: "blue" };
  return { text: "等待输入", tone: "green" };
}

export function transportName(transport?: ShareTransport) {
  return transport === "tailcat" ? "Tailcat 跨网络" : "局域网";
}

export function invitationText(
  token: string,
  transport: ShareTransport = "lan",
) {
  const connection =
    transport === "tailcat"
      ? "本邀请使用 Tailcat 跨网络连接（实验性），可能经第三方 DERP 中继。"
      : "双方需在同一局域网。";
  return `邀请你加入 Team Cross 协作。${connection}同一链接可供多人加入，有效至关闭、重置或本次共享结束。每位成员加入后独立参与。\n\n已安装 App：teamcross://join?invite=${encodeURIComponent(token)}\n\n安装说明：https://github.com/YTwsy/Team-Cross#安装\n安装后再次打开上方链接，或在“加入协作”中粘贴以下邀请码：\n${token}`;
}

export type MaterialReference = {
  materialId: string;
  version: number;
  turnId?: string;
};
export type MaterialItem = {
  id: string;
  type: string;
  text: string;
  notice?: string;
  sourceUtf16Length?: number;
  omittedUtf16Length?: number;
};
export type MaterialTurn = {
  id: string;
  status: string;
  items: MaterialItem[];
};
export type PublicationDraft = {
  id: string;
  hash: string;
  title: string;
  provider: Provider;
  sourceId: string;
  frozenAt: string;
  startTurnId: string;
  endTurnId: string;
  readingStartId?: string;
  turns: MaterialTurn[];
};
export type MaterialVersion = {
  version: number;
  title: string;
  provider: Provider;
  sourceId: string;
  startTurnId: string;
  endTurnId: string;
  readingStartId?: string;
  turnCount: number;
  hash: string;
  createdAt: string;
  noticeCount: number;
  changes?: { added: number; changed: number; removed: number };
};
export type Material = {
  id: string;
  authorId: string;
  author: string;
  withdrawnAt?: string;
  versions: MaterialVersion[];
};
export type MaterialPage = {
  materialId: string;
  version: MaterialVersion;
  scope: "stream" | "item";
  itemComplete?: boolean;
  pageEndsAtTurnBoundary: boolean;
  turns?: { id: string; label: string }[];
  segments: {
    turnId: string;
    itemId: string;
    type: string;
    text: string;
    notice?: string;
    startOffset: number;
    endOffset: number;
    length: number;
    sourceLength?: number;
    omittedLength?: number;
    collapsed?: boolean;
    readHint?: string;
  }[];
  nextCursor: string;
};
export type PublicationResult = {
  materialId: string;
  version: number;
  requestId: string;
  state: string;
  withdrawnAt?: string;
};
