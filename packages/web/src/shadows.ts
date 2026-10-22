export type ShadowMessage = {
  id: string;
  actor: { id: string; name: string };
  role: string;
  text: string;
  state: string;
  createdAt: string;
  receivedAt?: string;
  error?: string;
};
export type ShadowSettings = {
  model: string;
  modelProvider: string;
  reasoningEffort: string;
};
export type ShadowModels = {
  defaults: ShadowSettings;
  models: {
    model: string;
    displayName: string;
    defaultReasoningEffort: string;
    supportedReasoningEfforts: {
      reasoningEffort: string;
      description: string;
    }[];
    custom?: boolean;
  }[];
};
export type Shadow = {
  settings?: ShadowSettings;
  id: string;
  spaceId: string;
  name: string;
  owner: { id: string; name: string };
  enabled: boolean;
  online: boolean;
  busy: boolean;
  current: string;
  error?: string;
  model?: string;
  modelProvider?: string;
  reasoningEffort?: string;
  messages: ShadowMessage[] | null;
  segments:
    | {
        id: string;
        number: number;
        inputTokens: number;
        usageValid?: boolean;
        contextWindow: number;
        compactions: number;
      }[]
    | null;
  memories:
    | {
        id: string;
        text: string;
        scope: string;
        subjectId?: string;
        state: string;
        revision: number;
        sourceIds: string[];
      }[]
    | null;
  jobs:
    | {
        id: string;
        title: string;
        state: string;
        version: number;
        result?: string;
        error?: string;
      }[]
    | null;
};
export type ShadowView = {
  selfId: string;
  shadows: Shadow[];
  total: number;
  nextOffset?: number;
};
export type ShadowApproval = {
  id: string | number;
  method: string;
  params: Record<string, unknown>;
};
