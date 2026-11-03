import { t, tr } from "../i18n";
import { runtimeModeName, type Collaboration } from "../types";
import { Icon } from "./ui";

export function TechnicalInformation({
  collaboration: c,
  agentName,
}: {
  collaboration: Collaboration;
  agentName: string;
}) {
  return (
    <div className="context-technical">
      <div className="context-technical-heading">
        <span className="entry-icon">
          <Icon name="terminal" size={20} />
        </span>
        <div>
          <h3>{t("运行与会话")}</h3>
          <p>{t("当前协作记录与运行时最近确认的信息。")}</p>
        </div>
      </div>
      <dl>
        <div>
          <dt>{t("协作模式")}</dt>
          <dd>
            {runtimeModeName(c.runtimeMode)}
            {" " + t("· 创建后固定")}
          </dd>
        </div>
        <div>
          <dt>{t("原生客户端")}</dt>
          <dd>
            {agentName}
            {c.provider === "claude" ? t(" · 实验性") : ""}
          </dd>
        </div>
        <div>
          <dt>{t("协作 ID")}</dt>
          <dd>{c.id}</dd>
        </div>
        <div>
          <dt>{t("来源会话")}</dt>
          <dd>{c.sourceId}</dd>
        </div>
        <div>
          <dt>{t("协作会话")}</dt>
          <dd>{c.sessionId}</dd>
        </div>
        <div>
          <dt>{t("Git 分支")}</dt>
          <dd>{c.branch}</dd>
        </div>
        <div>
          <dt>{t("基线提交")}</dt>
          <dd>{c.head}</dd>
        </div>
        <div>
          <dt>{c.online ? t("当前模型") : t("最近确认的模型")}</dt>
          <dd>{c.model || tr`等待 ${agentName} 确认`}</dd>
        </div>
        <div>
          <dt>{t("推理强度")}</dt>
          <dd>{c.reasoningEffort || tr`${agentName} 默认`}</dd>
        </div>
      </dl>
    </div>
  );
}
