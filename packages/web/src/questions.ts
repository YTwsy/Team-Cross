import type { ActivitySource } from "./activity";
import { t } from "./i18n";

export type QuestionSummary = {
  id: string;
  text: string;
  delivery: string;
  state: string;
  source?: ActivitySource;
  totalTasks: number;
  activeTasks: number;
  relatedQuestions: number;
  updatedAt: string;
};
export type QuestionPage = {
  items: QuestionSummary[];
  total: number;
  revision: string;
  nextCursor?: string;
};
export type QuestionDetail = QuestionSummary & {
  condition: string;
  respondsTo: { source: ActivitySource; question?: string }[];
  tasks: {
    id: string;
    title: string;
    version: number;
    state: string;
    dependencyId: string;
    condition: string;
    dependencyState: string;
    nextStep?: string;
    active: boolean;
    answerReceived?: boolean;
    unavailable?: boolean;
  }[];
  totalConditions: number;
  revision: string;
  nextCursor?: string;
  error?: string;
};
export function questionLabel(state: string) {
  return (
    (
      {
        closed: t("关联任务已结束"),
        waiting: t("仍有条件等待答复"),
        answer_received: t("答复已到，待分身核对"),
        conditions_met: t("条件已满足，任务继续中"),
        delivery_unknown: t("发送状态待核对"),
        held_for_owner: t("本人已接回，尚未发送"),
        unavailable: t("来源暂不可用"),
      } as Record<string, string>
    )[state] || t("处理需要核对")
  );
}
