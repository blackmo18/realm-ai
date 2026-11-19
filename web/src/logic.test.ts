import type { Bot, Message } from './api';
import {
  applyMention, canJoinChat, isAnswered, mentionQuery, mentionSuggestions, mergeMessages,
  questionOptions, questionText, riskyTools, shortHandle, slugify,
} from './logic';

const bot = (handle: string, extra: Partial<Bot> = {}): Bot => ({
  handle, name: handle.split('/').pop()!, scope: handle.slice(1).split('/')[0], role: 'bot', status: 'active',
  provider: 'claude-cli', model: '', effort: '', charter: 'c', topics: [], skills: [], tools: [], lineage: [],
  knowledge_count: 0, dir: '', ...extra,
});
const msg = (id: string, extra: Partial<Message> = {}): Message => ({ id, chat: 'c', thread: id, from: '@user', kind: 'text', hop: 0, text: '', ts: '', ...extra });

describe('policy', () => {
  test('project bots join only their own project chats; global bots join anything', () => {
    expect(canJoinChat({ scope: 'app' }, 'app')).toBe(true);
    expect(canJoinChat({ scope: 'app' }, 'global')).toBe(false);
    expect(canJoinChat({ scope: 'app' }, 'web')).toBe(false);
    expect(canJoinChat({ scope: 'global' }, 'app')).toBe(true);
  });
  test('risky tools are flagged', () => {
    expect(riskyTools(['Read', 'Bash', 'Edit'])).toEqual(['Bash', 'Edit']);
    expect(riskyTools(['Read'])).toEqual([]);
  });
});

describe('mentions', () => {
  const bots = [bot('@app/db'), bot('@app/docs', { status: 'off' }), bot('@app/moderator', { role: 'moderator' }), bot('@app/old', { status: 'archived' }), bot('@global/ops')];
  test('detects the partial mention at the end of the text', () => {
    expect(mentionQuery('hi @d')).toBe('d');
    expect(mentionQuery('@')).toBe('');
    expect(mentionQuery('mail bob@x')).toBeNull();
    expect(mentionQuery('@db done')).toBeNull();
  });
  test('suggests matching bots, active first, never the moderator or archived', () => {
    expect(mentionSuggestions(bots, 'd').map((b) => b.handle)).toEqual(['@app/db', '@app/docs']);
    expect(mentionSuggestions(bots, '').map((b) => b.handle)).not.toContain('@app/moderator');
    expect(mentionSuggestions(bots, 'global/').map((b) => b.handle)).toEqual(['@global/ops']);
    expect(mentionSuggestions(bots, 'o').map((b) => b.handle)).toEqual(['@global/ops']);
  });
  test('applies the chosen handle', () => {
    expect(applyMention('ask @d', '@app/db')).toBe('ask @app/db ');
  });
});

describe('questions and messages', () => {
  const q = msg('05', { kind: 'question', parent: '01', text: 'Code or schema? Options: @app/reviewer, @app/db. Reply with one handle, e.g. @app/reviewer.' });
  test('parses moderator options and text', () => {
    expect(questionOptions(q)).toEqual(['@app/reviewer', '@app/db']);
    expect(questionText(q)).toBe('Code or schema?');
    expect(questionOptions(msg('x', { text: 'no options' }))).toEqual([]);
  });
  test('a question is answered once a later reply shares its source', () => {
    expect(isAnswered(q, [q])).toBe(false);
    expect(isAnswered(q, [q, msg('06', { parent: '01', kind: 'system', text: '→ @app/db' })])).toBe(true);
    expect(isAnswered(q, [q, msg('04', { parent: '01', kind: 'reply' })])).toBe(false);
  });
  test('merges streamed messages in id order without duplicates', () => {
    const merged = mergeMessages([msg('01'), msg('03')], msg('02'));
    expect(merged.map((m) => m.id)).toEqual(['01', '02', '03']);
    expect(mergeMessages(merged, msg('02'))).toBe(merged);
  });
  test('slugify and short handles', () => {
    expect(slugify(' My Release!! ')).toBe('my-release');
    expect(shortHandle('@app/db', 'app')).toBe('@db');
    expect(shortHandle('@global/ops', 'app')).toBe('@global/ops');
  });
});
