export type Mode = "existing" | "worktree";
export type Annotation = {
  id: string;
  text: string;
  reference?: string;
  author: string;
  createdAt: string;
};
export type Collaboration = {
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
  writer: "owner" | "remote";
  state: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
  online: boolean;
  busy: boolean;
  sharing: boolean;
  connected: boolean;
  client?: string;
  approvals: number;
  epoch: number;
  sequence: number;
  invitation?: string;
  expiresAt?: string;
  annotations: Annotation[];
  model: string;
};
export type Source = {
  id: string;
  name?: string;
  preview: string;
  cwd: string;
  updatedAt: number;
  status?: { type: string };
};
export type Preview = {
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
export type Info = {
  host: string;
  version: string;
  binary: string;
  codexVersion: string;
  codexError: string;
  desktopApp: string;
  dataDir: string;
  mcpCommand: string;
  mcpConfigured: boolean;
};
export type ClientPlan = { command: string; launched: boolean; note?: string };
export type History = {
  thread: {
    turns?: {
      id: string;
      status: string;
      items?: {
        type: string;
        text?: string;
        content?: { type: string; text?: string }[];
      }[];
    }[];
  };
};
export type Changes = { stat: string; diff: string; status: string };
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
  if (!c.online) return { text: "等待连接", tone: "muted" };
  if (c.approvals > 0) return { text: "等待审批", tone: "warning" };
  if (c.busy) return { text: "运行中", tone: "blue" };
  return { text: "等待输入", tone: "green" };
}
