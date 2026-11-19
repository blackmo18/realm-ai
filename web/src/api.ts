// Typed client for the realm daemon web API (served under /api).

export type Status = 'active' | 'off' | 'archived';

export interface Chat {
  id: string;
  kind: 'group' | 'dm';
  scope: string;
  name: string;
  routing: 'moderated' | 'mention';
  created: string;
}

export interface Message {
  id: string;
  chat: string;
  thread: string;
  parent?: string;
  from: string;
  kind: 'text' | 'command' | 'inquiry' | 'reply' | 'refusal' | 'system' | 'question';
  mentions?: string[];
  hop: number;
  text: string;
  ts: string;
}

export interface Lineage {
  inherited_from: string;
  at: string;
}

export interface Bot {
  handle: string;
  name: string;
  scope: string;
  role: 'bot' | 'moderator';
  status: Status;
  provider: string;
  model: string;
  effort: string;
  charter: string;
  topics: string[];
  skills: string[];
  tools: string[];
  lineage: Lineage[];
  knowledge_count: number;
  dir: string;
}

export interface BotInput {
  scope: string;
  name: string;
  provider?: string;
  model?: string;
  effort?: string;
  charter: string;
  topics?: string[];
  skills?: string[];
  tools?: string[];
  inherit?: string;
}

export type BotPatch = Partial<Pick<BotInput, 'provider' | 'model' | 'effort' | 'charter' | 'topics' | 'skills' | 'tools'>>;

export interface Entry {
  id: string;
  title: string;
  body: string;
  created: string;
  origin?: string;
}

export interface Project {
  slug: string;
  root: string;
}

export interface FolderInfo {
  path: string;
  exists: boolean;
  is_git: boolean;
  has_claude_md: boolean;
  skill_count: number;
  registered: boolean;
  slug: string;
}

export interface Template {
  name: string;
  model: string;
  effort: string;
  charter: string;
  topics: string[];
}

export interface Skill {
  name: string;
  source: 'global' | 'project';
  description: string;
}

export interface ProviderInfo {
  name: string;
  type: string;
  key_env: string;
  ready: boolean;
  note: string;
}

export interface Settings {
  home: string;
  moderator_model: string;
  limits: { max_concurrent: number; max_hops: number; history_messages: number; turn_timeout_sec: number };
  max_fanout_per_turn: number;
  max_turns_per_message: number;
}

export interface OnboardInput {
  dir: string;
  slug?: string;
  ignore_db: boolean;
  starters: string[];
  global_members: string[];
  chat_name: string;
  routing: 'moderated' | 'mention';
}

export interface OnboardResult {
  project: string;
  root: string;
  bots: string[];
  chat: Chat;
}

const TOKEN_KEY = 'realm-token';

/** Reads the token from the URL fragment once, then keeps it for the tab. */
export function initToken(): string {
  const match = window.location.hash.match(/token=([0-9a-f]+)/);
  if (match) {
    try {
      sessionStorage.setItem(TOKEN_KEY, match[1]);
    } catch {
      // storage blocked: the token still works for this page load
    }
    history.replaceState(null, '', window.location.pathname + window.location.search);
    currentToken = match[1];
    return currentToken;
  }
  try {
    currentToken = sessionStorage.getItem(TOKEN_KEY) ?? '';
  } catch {
    currentToken = '';
  }
  return currentToken;
}

let currentToken = '';

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

async function call<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    headers: { Authorization: `Bearer ${currentToken}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let env: { ok: boolean; data?: T; error?: string };
  try {
    env = await res.json();
  } catch {
    throw new ApiError(`Realm daemon returned ${res.status}`, res.status);
  }
  if (!env.ok) throw new ApiError(env.error ?? 'request failed', res.status);
  return env.data as T;
}

const qs = (params: Record<string, string>) => '?' + new URLSearchParams(params).toString();

export const api = {
  projects: () => call<Project[] | null>('GET', '/v1/projects').then((p) => p ?? []),
  chats: (scope: string) => call<Chat[] | null>('GET', '/v1/chats' + qs({ scope })).then((c) => c ?? []),
  chat: (id: string) => call<Chat>('GET', '/v1/chat' + qs({ id })),
  messages: (chat: string, limit = 100) =>
    call<Message[] | null>('GET', '/v1/messages' + qs({ chat, limit: String(limit) })).then((m) => m ?? []),
  members: (chat: string) => call<Bot[]>('GET', '/v1/chats/members' + qs({ chat })),
  post: (chat: string, text: string) => call<Message>('POST', '/v1/messages', { chat, text }),
  dm: (to: string, scope: string, text: string) => call<Message>('POST', '/v1/dm', { to, scope, text }),
  pick: (chat: string, bot: string, ask = '') => call<string>('POST', '/v1/pick', { chat, ask, bot }),
  reroute: (chat: string, message: string, bot: string) => call<string>('POST', '/v1/reroute', { chat, message, bot }),
  createChat: (scope: string, name: string, routing: string, members: string[]) =>
    call<Chat>('POST', '/v1/chats', { scope, name, routing, members }),
  addMember: (chat: string, bot: string) => call<string>('POST', '/v1/chats/members', { chat, bot }),
  setRouting: (chat: string, routing: string) => call<string>('POST', '/v1/chats/routing', { chat, routing }),
  bots: (scope: string) => call<Bot[]>('GET', '/v1/bots' + qs({ scope })),
  bot: (handle: string) => call<Bot>('GET', '/v1/bot' + qs({ handle, scope: '' })),
  createBot: (input: BotInput) => call<Bot>('POST', '/v1/bots', input),
  updateBot: (handle: string, patch: BotPatch) => call<Bot>('POST', '/v1/bots/update', { handle, patch }),
  setStatus: (handle: string, action: 'on' | 'off' | 'archive' | 'restore') =>
    call<{ bot: Bot; dispatched: number }>('POST', '/v1/bots/status', { handle, action }),
  inherit: (handle: string, from: string) => call<number>('POST', '/v1/bots/inherit', { handle, from }),
  knowledge: (handle: string) => call<Entry[]>('GET', '/v1/knowledge' + qs({ handle })),
  addKnowledge: (handle: string, title: string, body: string) => call<Entry>('POST', '/v1/knowledge', { handle, title, body }),
  updateKnowledge: (handle: string, id: string, title: string, body: string) =>
    call<Entry>('POST', '/v1/knowledge/update', { handle, id, title, body }),
  forgetKnowledge: (handle: string, id: string) => call<string>('POST', '/v1/knowledge/forget', { handle, id }),
  inspect: (path: string) => call<FolderInfo>('GET', '/v1/projects/inspect' + qs({ path })),
  suggest: () => call<string[]>('GET', '/v1/projects/suggest'),
  templates: () => call<Template[]>('GET', '/v1/templates'),
  onboard: (input: OnboardInput) => call<OnboardResult>('POST', '/v1/onboard', input),
  skills: (scope: string) => call<Skill[]>('GET', '/v1/skills' + qs({ scope })),
  providers: () => call<ProviderInfo[]>('GET', '/v1/providers'),
  settings: () => call<Settings>('GET', '/v1/settings'),
  /** Live messages for a chat. Returns a function that closes the stream. */
  stream: (chat: string, onMessage: (m: Message) => void): (() => void) => {
    const src = new EventSource('/api/v1/stream' + qs({ chat, token: currentToken }));
    src.onmessage = (ev) => {
      try {
        onMessage(JSON.parse(ev.data) as Message);
      } catch {
        // ignore malformed frames
      }
    };
    return () => src.close();
  },
};
