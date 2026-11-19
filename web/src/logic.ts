// Pure helpers shared by pages; unit-tested in logic.test.ts.
import type { Bot, Message } from './api';

export const GLOBAL = 'global';

export const RISKY_TOOLS = ['Edit', 'Write', 'Bash'] as const;
export const BUILTIN_TOOLS = ['Read', 'Grep', 'Glob', 'WebFetch', 'WebSearch', 'Edit', 'Write', 'Bash'] as const;
export const EFFORTS = ['', 'low', 'medium', 'high', 'xhigh', 'max'] as const;

/** Mirrors internal/policy: project bots never join global or other projects' chats. */
export function canJoinChat(bot: Pick<Bot, 'scope'>, chatScope: string): boolean {
  return bot.scope === GLOBAL || bot.scope === chatScope;
}

export function riskyTools(tools: string[]): string[] {
  return tools.filter((t) => (RISKY_TOOLS as readonly string[]).includes(t));
}

/** The @word being typed at the end of the composer text, or null. */
export function mentionQuery(text: string): string | null {
  const m = text.match(/(?:^|\s)@([a-z0-9/-]*)$/);
  return m ? m[1] : null;
}

/** Bots matching a partial mention, active ones first, max 6. */
export function mentionSuggestions(bots: Bot[], query: string): Bot[] {
  const q = query.toLowerCase();
  return bots
    .filter((b) => b.role !== 'moderator' && b.status !== 'archived')
    .filter((b) => b.name.startsWith(q) || b.handle.slice(1).startsWith(q))
    .sort((a, b) => Number(a.status !== 'active') - Number(b.status !== 'active') || a.name.localeCompare(b.name))
    .slice(0, 6);
}

/** Replaces the trailing partial mention with the chosen handle. */
export function applyMention(text: string, handle: string): string {
  return text.replace(/@([a-z0-9/-]*)$/, handle + ' ');
}

/** Handle shown for a bot in a given scope: short inside its own scope. */
export function shortHandle(handle: string, scope: string): string {
  const prefix = '@' + scope + '/';
  return handle.startsWith(prefix) ? '@' + handle.slice(prefix.length) : handle;
}

/** Options from a moderator question ("... Options: @a, @b. Reply ..."). */
export function questionOptions(m: Message): string[] {
  const match = m.text.match(/Options: ([^.]+)\./);
  if (!match) return [];
  return match[1].split(',').map((s) => s.trim()).filter((s) => s.startsWith('@'));
}

/** Question text without the options suffix. */
export function questionText(m: Message): string {
  return m.text.replace(/\s*Options: .*$/, '');
}

/** True once a later message answered (parented to) the question's source. */
export function isAnswered(question: Message, all: Message[]): boolean {
  return all.some((m) => m.parent === question.parent && m.id > question.id && (m.kind === 'reply' || m.kind === 'system'));
}

export function slugify(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '');
}

export function mergeMessages(current: Message[], incoming: Message): Message[] {
  if (current.some((m) => m.id === incoming.id)) return current;
  return [...current, incoming].sort((a, b) => (a.id < b.id ? -1 : 1));
}

export function timeOf(ts: string): string {
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
