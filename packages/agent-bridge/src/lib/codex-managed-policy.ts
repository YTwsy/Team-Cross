import type { JsonlRpcProcess } from "./jsonl-rpc-client.js";

// These are independently enforced tool/automation surfaces, not filesystem
// permission-profile settings. Require runtime discovery; unknown flags fail.
export const MANAGED_DISABLED_FEATURES = [
  "plugins", "apps", "enable_mcp_apps", "remote_plugin", "recommended_plugins",
  "browser_use", "browser_use_external", "browser_use_full_cdp_access", "in_app_browser",
  "computer_use", "hooks", "plugin_hooks", "skill_mcp_dependency_install",
  "image_generation", "artifact", "memories", "multi_agent", "multi_agent_v2", "enable_fanout", "goals",
  "code_mode", "js_repl", "remote_control", "in_app_local_automation",
] as const;
// The local Code Mode host is the execution engine for models whose tool_mode
// requires exec/wait, even when optional code_mode feature configuration is off.
// It does not authorize nested external tools; their independent gates stay off.
export const MANAGED_ENABLED_FEATURES = ["code_mode_host"] as const;
const MAX_MCP_SERVERS = 128;

export class CodexManagedPolicyError extends Error {}

export function managedToolConfig(mcpKeys: string[]): Record<string, unknown> {
  return {
    features: {
      ...Object.fromEntries(MANAGED_DISABLED_FEATURES.map((name) => [name, false])),
      ...Object.fromEntries(MANAGED_ENABLED_FEATURES.map((name) => [name, true])),
    },
    web_search: "disabled",
    allow_login_shell: false,
    mcp_servers: Object.fromEntries(mcpKeys.map((name) => [name, { enabled: false }])),
  };
}

export function managedToolArgs(mcpKeys: string[]): string[] {
  const config = [
    ...MANAGED_DISABLED_FEATURES.map((name) => `features.${name}=false`),
    ...MANAGED_ENABLED_FEATURES.map((name) => `features.${name}=true`),
    'web_search="disabled"', "allow_login_shell=false",
    // The empty table only guarantees shape for unconfigured installations; it
    // does not clear inherited servers. The metadata pass still discovers every
    // inherited key and the rebuilt process disables each one explicitly.
    // JSON string escaping is valid for the bounded TOML basic keys below.
    `mcp_servers={${mcpKeys.map((name) => `${JSON.stringify(name)}={enabled=false}`).join(",")}}`,
  ];
  return config.flatMap((value) => ["-c", value]);
}

export function safeManagedPolicyError(error: unknown): Error {
  return error instanceof CodexManagedPolicyError ? error
    : new CodexManagedPolicyError("Codex managed initialization or tool-scope verification failed; execution refused (provider diagnostic withheld)");
}

// The raw config may contain sensitive values. Do not retain, log, serialize,
// return or attach it to errors. Only return bounded names needed to disable MCP.
export async function readManagedToolMetadata(client: JsonlRpcProcess, cwd: string, requireDisabled: boolean): Promise<string[]> {
  try {
    const response = await client.request("config/read", { cwd, includeLayers: false });
    const config = record(response) && record(response.config) ? response.config : undefined;
    const features = config && record(config.features) ? config.features : undefined;
    if (!config || !features || !record(config.mcp_servers)
      || config.web_search !== "disabled" || config.allow_login_shell !== false
      || MANAGED_DISABLED_FEATURES.some((name) => features[name] !== false)
      || MANAGED_ENABLED_FEATURES.some((name) => features[name] !== true)) {
      throw new CodexManagedPolicyError("Codex managed tool configuration is missing or not restricted; execution refused");
    }
    const keys = Object.keys(config.mcp_servers);
    if (keys.length > MAX_MCP_SERVERS || keys.some((key) => !validMcpKey(key))) {
      throw new CodexManagedPolicyError("Codex MCP configuration exceeds the safe metadata limits; execution refused");
    }
    for (const key of keys) {
      const server = config.mcp_servers[key];
      if (!record(server) || (server.enabled !== undefined && typeof server.enabled !== "boolean")
        || (requireDisabled && server.enabled !== false)) {
        throw new CodexManagedPolicyError("Codex managed MCP configuration is active, changed, or unverifiable; execution refused");
      }
    }
    const requirements = await client.request("configRequirements/read", {});
    if (!record(requirements) || !(requirements.requirements === null || record(requirements.requirements))) {
      throw new CodexManagedPolicyError("Codex managed requirements are unverifiable; execution refused");
    }
    if (record(requirements.requirements) && requirements.requirements.hooks != null) {
      // Do not disable or ignore administrator-enforced hooks to obtain a pass.
      throw new CodexManagedPolicyError("Codex requires managed hooks outside this restricted execution contract; execution refused");
    }
    await verifyManagedRuntimeFeatures(client);
    return keys;
  } catch (error) { throw safeManagedPolicyError(error); }
}

export async function verifyManagedRuntimeFeatures(client: JsonlRpcProcess, threadId?: string): Promise<void> {
  try {
    const found = new Set<string>();
    await boundedInventory(client, "experimentalFeature/list", threadId, (value) => {
      if (typeof value.name !== "string") throw new CodexManagedPolicyError("Codex runtime feature metadata is incompatible; execution refused");
      const disabled = (MANAGED_DISABLED_FEATURES as readonly string[]).includes(value.name);
      const enabled = (MANAGED_ENABLED_FEATURES as readonly string[]).includes(value.name);
      if (disabled || enabled) {
        if (value.enabled !== enabled || found.has(value.name)) throw new CodexManagedPolicyError("Codex runtime tool or local execution-host feature is incompatible or unverifiable; execution refused");
        found.add(value.name);
      }
    });
    if (found.size !== MANAGED_DISABLED_FEATURES.length + MANAGED_ENABLED_FEATURES.length) throw new CodexManagedPolicyError("Codex lacks required runtime tool capability metadata; execution refused");
  } catch (error) { throw safeManagedPolicyError(error); }
}

export async function verifyManagedLoadedTools(client: JsonlRpcProcess, threadId: string): Promise<void> {
  try {
    await verifyManagedRuntimeFeatures(client, threadId);
    await boundedInventory(client, "mcpServerStatus/list", threadId, (value) => {
      if (value.runtimeStatus !== "disabled" || !record(value.tools) || Object.keys(value.tools).length
        || !Array.isArray(value.resources) || value.resources.length
        || !Array.isArray(value.resourceTemplates) || value.resourceTemplates.length) {
        throw new CodexManagedPolicyError("Codex loaded Session contains active or unverifiable MCP tools; execution refused");
      }
    });
  } catch (error) { throw safeManagedPolicyError(error); }
}

async function boundedInventory(client: JsonlRpcProcess, method: string, threadId: string | undefined, accept: (value: Record<string, unknown>) => void): Promise<void> {
  const seen = new Set<string>();
  let cursor: string | undefined;
  let count = 0;
  for (let page = 0; page < 4; page++) {
    const response = await client.request(method, {
      limit: 200, ...(threadId === undefined ? {} : { threadId }),
      ...(cursor === undefined ? {} : { cursor }),
      ...(method === "mcpServerStatus/list" ? { detail: "toolsAndAuthOnly" } : {}),
    });
    if (!record(response) || !Array.isArray(response.data)) throw new CodexManagedPolicyError("Codex runtime tool inventory is incompatible; execution refused");
    count += response.data.length;
    if (count > 512) throw new CodexManagedPolicyError("Codex runtime tool inventory exceeds safe limits; execution refused");
    for (const value of response.data) {
      if (!record(value)) throw new CodexManagedPolicyError("Codex runtime tool inventory is incompatible; execution refused");
      accept(value);
    }
    if (response.nextCursor === null) return;
    if (typeof response.nextCursor !== "string" || !response.nextCursor || response.nextCursor.length > 4096 || seen.has(response.nextCursor)) {
      throw new CodexManagedPolicyError("Codex runtime tool inventory has an invalid cursor; execution refused");
    }
    cursor = response.nextCursor; seen.add(cursor);
  }
  throw new CodexManagedPolicyError("Codex runtime tool inventory is incomplete; execution refused");
}

function validMcpKey(value: string): boolean {
  return value.length > 0 && value.length <= 128 && !/[\x00-\x1f\x7f\ud800-\udfff]/u.test(value);
}
function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
