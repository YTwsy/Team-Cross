import fs from "node:fs";
import path from "node:path";
import ts from "typescript";
import english from "../src/en.json" with { type: "json" };

const root = path.resolve(import.meta.dirname, "../src");
const han = /[\u3400-\u9fff]/;
const errors = [];
const used = new Set();
const placeholders = (value) =>
  [...value.matchAll(/\{(\d+)\}/g)].map((part) => part[1]).sort().join(",");

function checkKey(key, file, position) {
  if (!han.test(key)) return;
  used.add(key);
  const translated = english[key];
  if (typeof translated !== "string" || !translated.trim()) {
    errors.push(`${file}:${position} missing English: ${key}`);
  } else if (han.test(translated)) {
    errors.push(`${file}:${position} contains Chinese in English: ${key}`);
  } else if (placeholders(key) !== placeholders(translated)) {
    errors.push(`${file}:${position} placeholder mismatch: ${key}`);
  }
}

function walk(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === "test") continue;
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      walk(file);
      continue;
    }
    if (!/\.tsx?$/.test(file) || file.endsWith("/i18n.ts")) continue;
    const source = fs.readFileSync(file, "utf8");
    const tree = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
    const relative = path.relative(root, file);
    function inspect(node) {
      const position = tree.getLineAndCharacterOfPosition(node.getStart(tree)).line + 1;
      const translatedCall =
        (ts.isCallExpression(node) && node.expression.getText(tree) === "t") ||
        (ts.isTaggedTemplateExpression(node) && node.tag.getText(tree) === "tr");
      if (translatedCall) {
        let insideFunction = false;
        for (let parent = node.parent; parent; parent = parent.parent) {
          if (ts.isFunctionLike(parent)) {
            insideFunction = true;
            break;
          }
        }
        if (!insideFunction) errors.push(`${relative}:${position} translation evaluated at module load`);
      }
      if (ts.isCallExpression(node) && (node.expression.getText(tree) === "t" || node.expression.getText(tree) === "serviceText")) {
        const literal = node.arguments[node.expression.getText(tree) === "t" ? 0 : 1];
        if (literal && (ts.isStringLiteral(literal) || ts.isNoSubstitutionTemplateLiteral(literal))) {
          checkKey(literal.text, relative, position);
        }
      }
      if (ts.isTaggedTemplateExpression(node) && node.tag.getText(tree) === "tr") {
        const template = node.template;
        const key = ts.isNoSubstitutionTemplateLiteral(template)
          ? template.text
          : template.head.text + template.templateSpans.map((span, index) => `{${index}}${span.literal.text}`).join("");
        checkKey(key, relative, position);
      }
      if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
        if (han.test(node.text) && !(ts.isCallExpression(node.parent) && (node.parent.expression.getText(tree) === "t" || node.parent.expression.getText(tree) === "serviceText")) && !(ts.isTaggedTemplateExpression(node.parent) && node.parent.tag.getText(tree) === "tr")) {
          errors.push(`${relative}:${position} untranslated string: ${node.text}`);
        }
      }
      if (ts.isTemplateExpression(node) && (han.test(node.head.text) || node.templateSpans.some((span) => han.test(span.literal.text))) && !(ts.isTaggedTemplateExpression(node.parent) && node.parent.tag.getText(tree) === "tr")) {
        errors.push(`${relative}:${position} untranslated template`);
      }
      if (ts.isJsxText(node) && han.test(node.text)) {
        errors.push(`${relative}:${position} untranslated JSX text: ${node.text.trim()}`);
      }
      ts.forEachChild(node, inspect);
    }
    inspect(tree);
  }
}

walk(root);
if (errors.length) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Checked ${used.size} translated UI messages.`);
}
