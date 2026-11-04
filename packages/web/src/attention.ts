import type { ActivityItem, ActivitySource } from "./activity";
import { t } from "./i18n";

export type ActivityProcessing = {
  details?: boolean;
  state: string;
  shadowId?: string;
  shadowName?: string;
  hold?: string;
  error?: string;
  receivedAt?: string;
  jobs?: {
    id: string;
    shadowId: string;
    title: string;
    state: string;
    version: number;
    waiting?: string;
  }[];
  actions?: {
    id: string;
    tool: string;
    state: string;
    receiptState?: string;
    sources?: ActivitySource[];
    updatedAt: string;
  }[];
  totalJobs: number;
  totalActions: number;
};

// A receipt may point to another discussion. Open that exact source without
// borrowing the original event's preview, follow subject or read identity.
export function processingSourceItem(source: ActivitySource): ActivityItem {
  return {
    id: `processing:${JSON.stringify(source)}`,
    seq: 0,
    kind: source.annotationId
      ? "reply"
      : source.materialId
        ? "material"
        : "turn",
    source,
    subject: {
      kind: source.annotationId
        ? "discussion"
        : source.materialId
          ? "material"
          : "execution",
      id: source.annotationId || source.materialId || source.executionId || "",
      executionId: source.executionId,
    },
    actor: { kind: "", name: "" },
    createdAt: "",
    preview: "",
    reasons: [],
    unread: false,
    preserveReadState: true,
  };
}
export function processingLabel(state: string) {
  return (
    (
      {
        no_shadow: t("本机尚无分身接手"),
        unreceived: t("等待分身接收"),
        received: t("分身已接收"),
        processing: t("分身正在处理"),
        processed: t("分身已处理"),
        completed: t("关联任务已完成"),
        needs_owner: t("待你回答"),
        waiting_others: t("等待同事或工作结果"),
        error: t("处理需要核对"),
        paused: t("任务已暂停"),
        skipped: t("按当前关注设置停止处理"),
      } as Record<string, string>
    )[state] || t("处理状态待核对")
  );
}
export function processingHold(hold?: string) {
  return (
    (
      {
        shadow_offline: t("默认或原处理分身暂不可用"),
        shadow_paused: t("分身已暂停"),
        shadow_error: t("分身运行需要核对"),
        shadow_starting: t("分身正在连接"),
        route_paused: t("自动接手已暂停"),
        focus_paused: t("这项关注已暂停"),
        delivery_unknown: t("协作送达待核对"),
      } as Record<string, string>
    )[hold || ""] || ""
  );
}
export function jobLabel(state: string) {
  return (
    (
      {
        waiting: t("等待新信息"),
        queued: t("等待接续"),
        running: t("正在执行"),
        completed: t("已完成"),
        paused: t("已暂停"),
        needs_followup: t("需要继续处理"),
        stopping: t("正在停止"),
        cancelled: t("已取消"),
        failed: t("执行失败"),
        interrupted: t("执行已中断"),
        unknown: t("结果待核对"),
      } as Record<string, string>
    )[state] || t("处理状态待核对")
  );
}
