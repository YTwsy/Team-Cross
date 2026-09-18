import { createHighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";

// Loaded as a separate local chunk; reading never downloads code from a CDN.
const ready = createHighlighterCore({
  engine: createJavaScriptRegexEngine(),
  themes: [
    import("shiki/themes/github-light.mjs"),
    import("shiki/themes/github-dark.mjs"),
  ],
  langs: [
    import("shiki/langs/go.mjs"),
    import("shiki/langs/typescript.mjs"),
    import("shiki/langs/javascript.mjs"),
    import("shiki/langs/json.mjs"),
    import("shiki/langs/bash.mjs"),
    import("shiki/langs/yaml.mjs"),
    import("shiki/langs/python.mjs"),
    import("shiki/langs/swift.mjs"),
    import("shiki/langs/diff.mjs"),
  ],
});
export async function syntaxHighlighter() {
  const highlighter = await ready;
  return (text: string, language: string) => {
    if (
      text.length > 64000 ||
      !highlighter.getLoadedLanguages().includes(language)
    )
      return;
    return highlighter.codeToTokens(text, {
      lang: language,
      themes: { light: "github-light", dark: "github-dark" },
    }).tokens;
  };
}
