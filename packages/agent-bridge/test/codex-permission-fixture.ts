import { MANAGED_DISABLED_FEATURES, MANAGED_ENABLED_FEATURES } from "../src/lib/codex-managed-policy.js";

// Synthetic protocol metadata, never a native capability acceptance fixture.
export function verifiedThreadResponse(input: unknown, id = "thread-1") {
  const params = input as Record<string, unknown>;
  return {
    thread: { id },
    model: params.model ?? "synthetic-model",
    cwd: params.cwd,
    runtimeWorkspaceRoots: params.runtimeWorkspaceRoots,
    approvalPolicy: params.approvalPolicy,
    activePermissionProfile: { id: params.permissions, extends: ":workspace" },
    sandbox: {
      type: "workspaceWrite", writableRoots: [] as string[],
      networkAccess: String(params.permissions).endsWith("-online"),
      excludeTmpdirEnvVar: true, excludeSlashTmp: true,
    },
  };
}
export function syntheticToolMetadata(method: string): unknown {
  if (method === "config/read") return { config: {
    features: { ...Object.fromEntries(MANAGED_DISABLED_FEATURES.map((name) => [name, false])), ...Object.fromEntries(MANAGED_ENABLED_FEATURES.map((name) => [name, true])) },
    mcp_servers: {}, web_search: "disabled", allow_login_shell: false,
  } };
  if (method === "configRequirements/read") return { requirements: null };
  if (method === "experimentalFeature/list") return { data: [...MANAGED_DISABLED_FEATURES.map((name) => ({ name, enabled: false })), ...MANAGED_ENABLED_FEATURES.map((name) => ({ name, enabled: true }))], nextCursor: null };
  if (method === "mcpServerStatus/list") return { data: [], nextCursor: null };
  return undefined;
}

export function isToolMetadataMethod(method: string): boolean {
  return ["config/read", "configRequirements/read", "experimentalFeature/list", "mcpServerStatus/list"].includes(method);
}
