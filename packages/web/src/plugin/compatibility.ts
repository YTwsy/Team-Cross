import { APIError } from "../api";
import { t } from "../i18n";

export function supportsBridge(required: unknown, available: unknown) {
  const need = required === undefined ? 0 : required;
  const have = available === undefined ? 0 : available;
  return (
    typeof need === "number" &&
    Number.isSafeInteger(need) &&
    need >= 0 &&
    typeof have === "number" &&
    Number.isSafeInteger(have) &&
    have >= need
  );
}

export function reloadRequiredError() {
  return new APIError(
    t("插件连接需要重新加载"),
    "plugin_reload_required",
    t(
      "请先保存未发送的内容，再退出并重新打开 Codex 或 ChatGPT；仅刷新页面无法更新插件连接",
    ),
  );
}

declare global {
  interface Window {
    __teamcrossReloadNoticeCleanup?: () => void;
  }
}

export function clearReloadNotice() {
  window.__teamcrossReloadNoticeCleanup?.();
  delete window.__teamcrossReloadNoticeCleanup;
}

// A pre-contract loader may already be replacing the panel. Keep this notice
// outside React so its rollback restores the old editor without losing guidance.
// Reuse the existing banner style, including when that loader restores old CSS.
export function showReloadNotice() {
  clearReloadNotice();
  const banner = document.createElement("aside");
  banner.id = "teamcross-bridge-reload";
  banner.className = "plugin-update-banner";
  banner.setAttribute("role", "status");
  banner.style.display = "block";
  const title = document.createElement("strong");
  const details = document.createElement("p");
  details.style.margin = "4px 0 0";
  banner.append(title, details);
  // Older loaders otherwise keep showing an ineffective "refresh" action.
  const style = document.createElement("style");
  style.textContent =
    ".plugin-update-banner:not(#teamcross-bridge-reload){display:none!important}";
  document.head.append(style);
  const position = () => {
    const parent =
      [...document.querySelectorAll("dialog[open]")].at(-1) || document.body;
    if (banner.parentElement !== parent) parent.append(banner);
    const heading = t("插件连接需要重新加载");
    const message = t(
      "请先保存未发送的内容，再退出并重新打开 Codex 或 ChatGPT；仅刷新页面无法更新插件连接",
    );
    if (title.textContent !== heading) title.textContent = heading;
    if (details.textContent !== message) details.textContent = message;
    // A legacy panel may already have loaded newer UI before this fix existed.
    // Its rollback can still render the old raw route error. Explain precisely
    // this incompatibility without hiding unrelated failures or touching drafts.
    for (const box of document.querySelectorAll(".error-box")) {
      const text = box.querySelector("p");
      if (text?.textContent?.trim() !== "WebGUI route is not allowed") continue;
      text.textContent = message;
      const heading = box.querySelector("strong");
      if (heading) heading.textContent = t("插件连接需要重新加载");
    }
  };
  position();
  const observer = new MutationObserver(position);
  observer.observe(document.body, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ["open"],
  });
  window.__teamcrossReloadNoticeCleanup = () => {
    observer.disconnect();
    banner.remove();
    style.remove();
  };
}
