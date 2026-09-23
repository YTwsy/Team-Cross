import english from "./en.json";

export type LanguageMode = "auto" | "zh-CN" | "en";
export type InterfaceLanguage = "zh-CN" | "en";
export type LanguageInfo = { mode: LanguageMode; resolved: InterfaceLanguage };

let activeLanguage: InterfaceLanguage = "zh-CN";
let activeSetting: LanguageInfo = { mode: "auto", resolved: "zh-CN" };

export function languageInfo(): LanguageInfo {
  return activeSetting;
}

export function setLanguageInfo(value: LanguageInfo): boolean {
  const mode: LanguageMode =
    value.mode === "zh-CN" || value.mode === "en" ? value.mode : "auto";
  const resolved: InterfaceLanguage =
    value.resolved === "zh-CN" ? "zh-CN" : "en";
  const changed =
    mode !== activeSetting.mode || resolved !== activeSetting.resolved;
  activeSetting = { mode, resolved };
  setLanguage(resolved);
  return changed;
}

export function language(): InterfaceLanguage {
  return activeLanguage;
}

export function setLanguage(value: string): boolean {
  const next: InterfaceLanguage = value === "zh-CN" ? "zh-CN" : "en";
  document.documentElement.lang = next;
  if (next === activeLanguage) return false;
  activeLanguage = next;
  activeSetting = { ...activeSetting, resolved: next };
  return true;
}

export function browserLanguage(): InterfaceLanguage {
  const preferred = navigator.languages?.[0] || navigator.language || "";
  return /^zh(?:[-_]|$)/i.test(preferred) ? "zh-CN" : "en";
}

export function t(source: string): string {
  if (activeLanguage === "zh-CN") return source;
  return (english as Record<string, string>)[source] || source;
}

// Use only for Core-owned diagnostics and notices, never session or user text.
export function serviceText(
  source?: string,
  fallback = "请查看诊断信息。",
): string {
  if (!source || activeLanguage === "zh-CN" || !/[\u3400-\u9fff]/.test(source))
    return source || "";
  const translated = (english as Record<string, string>)[source];
  if (translated) return translated;
  const omitted = source.match(
    /^原始内容过长，发布时省略 (\d+) 个 UTF-16 字符；保留头尾$/,
  );
  if (omitted)
    return `Source content was too long; ${omitted[1]} UTF-16 characters were omitted on publication. The beginning and end were kept.`;
  return t(fallback);
}

export function tr(
  strings: TemplateStringsArray,
  ...values: unknown[]
): string {
  const key = strings.reduce(
    (result, part, index) => result + (index ? `{${index - 1}}` : "") + part,
    "",
  );
  const translated = t(key);
  return translated.replace(/\{(\d+)\}/g, (placeholder, index: string) => {
    const value = values[Number(index)];
    return value === undefined ? placeholder : String(value);
  });
}

export function formatDate(
  value: string | number | Date,
  options?: Intl.DateTimeFormatOptions,
): string {
  return new Intl.DateTimeFormat(activeLanguage, options).format(
    new Date(value),
  );
}
