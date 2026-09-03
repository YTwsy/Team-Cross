import type { SealedCodeReview } from "../types";

export const codeReview: SealedCodeReview = {
  roundId: "round-1",
  baseline: "abc123",
  patch: `diff --git a/src/retry.ts b/src/retry.ts
--- a/src/retry.ts
+++ b/src/retry.ts
@@ -1,2 +1,2 @@
 const retries = 1;
-export const connected = false;
+export const connected = true;`,
  lines: [
    {
      kind: "context",
      text: " const retries = 1;",
      oldPath: "src/retry.ts",
      newPath: "src/retry.ts",
      oldLine: 1,
      newLine: 1,
    },
    {
      kind: "delete",
      text: "-export const connected = false;",
      oldPath: "src/retry.ts",
      newPath: "src/retry.ts",
      oldLine: 2,
    },
    {
      kind: "add",
      text: "+export const connected = true;",
      oldPath: "src/retry.ts",
      newPath: "src/retry.ts",
      newLine: 2,
    },
  ],
};
