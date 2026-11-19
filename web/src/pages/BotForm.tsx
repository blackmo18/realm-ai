import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type Bot, type BotInput } from '../api';
import { BUILTIN_TOOLS, EFFORTS, GLOBAL, RISKY_TOOLS, riskyTools } from '../logic';
import { navigate } from '../router';
import { Icon } from '../components/Icon';

type Props = { scope: string; edit?: string };

const MODEL_HINTS: Record<string, string> = {
  'claude-cli': 'haiku, sonnet, opus',
  anthropic: 'claude-sonnet-5-5, claude-haiku-5-5',
  openai: 'gpt-5, gpt-5-mini',
  openrouter: 'any OpenRouter model id, e.g. openai/gpt-5',
  ollama: 'any pulled model, e.g. llama3.2',
};

type Draft = Required<Omit<BotInput, 'inherit'>> & { inherit: string };

function draftFrom(scope: string, bot?: Bot): Draft {
  return {
    scope: bot?.scope ?? scope,
    name: bot?.name ?? '',
    provider: bot?.provider ?? 'claude-cli',
    model: bot?.model ?? '',
    effort: bot?.effort ?? '',
    charter: bot?.charter ?? '',
    topics: bot?.topics ?? [],
    skills: bot?.skills ?? [],
    tools: bot?.tools ?? ['Read', 'Grep', 'Glob'],
    inherit: '',
  };
}

export function BotForm({ scope, edit }: Props) {
  const existing = useQuery({ queryKey: ['bot', edit], queryFn: () => api.bot(edit!), enabled: !!edit });
  if (edit && !existing.data) {
    return <main className="page">{existing.isError ? <p className="error">{String(existing.error)}</p> : <p className="sub">Loading…</p>}</main>;
  }
  return <BotFormInner key={edit ?? 'new'} initial={draftFrom(scope, existing.data)} isEdit={!!edit} handle={edit} />;
}

function BotFormInner({ initial, isEdit, handle }: { initial: Draft; isEdit: boolean; handle?: string }) {
  const qc = useQueryClient();
  const [d, setD] = useState<Draft>(initial);
  const [topicDraft, setTopicDraft] = useState('');
  const projects = useQuery({ queryKey: ['projects'], queryFn: api.projects });
  const providers = useQuery({ queryKey: ['providers'], queryFn: api.providers });
  const skills = useQuery({ queryKey: ['skills', d.scope], queryFn: () => api.skills(d.scope) });
  const sources = useQuery({ queryKey: ['bots', d.scope], queryFn: () => api.bots(d.scope), enabled: !isEdit });
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((cur) => ({ ...cur, [k]: v }));
  const toggle = (k: 'tools' | 'skills', v: string) => setD((cur) => ({ ...cur, [k]: cur[k].includes(v) ? cur[k].filter((x) => x !== v) : [...cur[k], v] }));
  const risky = riskyTools(d.tools);
  const preview = d.scope === GLOBAL ? `@global/${d.name || 'name'}` : `@${d.scope}/${d.name || 'name'}`;

  const save = useMutation({
    mutationFn: async () => {
      if (isEdit && handle) {
        const { provider, model, effort, charter, topics, skills: sk, tools } = d;
        return api.updateBot(handle, { provider, model, effort, charter, topics, skills: sk, tools });
      }
      const { inherit, ...rest } = d;
      return api.createBot({ ...rest, inherit: inherit || undefined });
    },
    onSuccess: (bot) => {
      void qc.invalidateQueries({ queryKey: ['bots'] });
      void qc.invalidateQueries({ queryKey: ['bot', bot.handle] });
      navigate({ page: 'bot', handle: bot.handle });
    },
  });
  const onSubmit = (e: FormEvent) => { e.preventDefault(); save.mutate(); };
  const addTopic = () => {
    const t = topicDraft.trim().toLowerCase();
    if (t && !d.topics.includes(t)) set('topics', [...d.topics, t]);
    setTopicDraft('');
  };

  return (
    <main className="page">
      <h1>{isEdit ? `Edit ${handle}` : 'New bot'}</h1>
      <form onSubmit={onSubmit} className="row" style={{ alignItems: 'flex-start', gap: 24 }}>
        <div style={{ flex: '3 1 520px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 18 }}>
          <div className="grid2">
            <div className="field">
              <label htmlFor="bf-name">Name</label>
              <input id="bf-name" className="input" required pattern="[a-z][a-z0-9-]{0,31}" value={d.name} disabled={isEdit} onChange={(e) => set('name', e.target.value.toLowerCase())} />
              <span className="hint">Handle: <span className="mono">{preview}</span></span>
            </div>
            <div className="field">
              <span className="label" id="bf-scope">Scope</span>
              <div className="seg" role="group" aria-labelledby="bf-scope">
                {[GLOBAL, ...(projects.data ?? []).map((p) => p.slug)].map((s) => (
                  <button key={s} type="button" aria-pressed={d.scope === s} disabled={isEdit} onClick={() => set('scope', s)}>{s === GLOBAL ? 'Global' : s}</button>
                ))}
              </div>
              <span className="hint">{d.scope === GLOBAL ? 'Works in global chats and any project chat it joins.' : 'Runs in the project folder; sees its CLAUDE.md and skills.'}</span>
            </div>
          </div>
          <div className="field">
            <label htmlFor="bf-charter">Charter <span className="sub">— the only work this bot acts on</span></label>
            <textarea id="bf-charter" className="input" required value={d.charter} onChange={(e) => set('charter', e.target.value)} />
          </div>
          <div className="grid3">
            <div className="field">
              <label htmlFor="bf-provider">Runtime</label>
              <select id="bf-provider" className="input" value={d.provider} onChange={(e) => set('provider', e.target.value)}>
                {(providers.data ?? [{ name: 'claude-cli', type: 'subscription', ready: true, note: '', key_env: '' }]).map((p) => (
                  <option key={p.name} value={p.name}>{p.name} · {p.type}{p.ready ? '' : ' (not ready)'}</option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="bf-model">Model</label>
              <input id="bf-model" className="input mono" value={d.model} placeholder="default" onChange={(e) => set('model', e.target.value)} />
              <span className="hint">Suggested: {MODEL_HINTS[d.provider] ?? 'provider model id'}</span>
            </div>
            <div className="field">
              <label htmlFor="bf-effort">Effort</label>
              <select id="bf-effort" className="input" value={d.effort} onChange={(e) => set('effort', e.target.value)}>
                {EFFORTS.map((x) => <option key={x} value={x}>{x || 'default'}</option>)}
              </select>
            </div>
          </div>
          <div className="field">
            <label htmlFor="bf-topic">Topics <span className="sub">— help the moderator and refusal suggestions find this bot</span></label>
            <div className="row">
              {d.topics.map((t) => (
                <button key={t} type="button" className="chip" aria-label={`Remove topic ${t}`} onClick={() => set('topics', d.topics.filter((x) => x !== t))}>{t} ×</button>
              ))}
              <input id="bf-topic" className="input" style={{ width: 160 }} value={topicDraft} placeholder="add topic" onChange={(e) => setTopicDraft(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); addTopic(); } }} onBlur={addTopic} />
            </div>
          </div>
          <fieldset>
            <legend>Built-in tools (claude-cli)</legend>
            <div className="grid3">
              {BUILTIN_TOOLS.map((t) => {
                const isRisky = (RISKY_TOOLS as readonly string[]).includes(t);
                const on = d.tools.includes(t);
                return (
                  <label key={t} className={`check ${on ? 'on' : ''} ${isRisky ? 'risky' : ''}`}>
                    <input type="checkbox" checked={on} onChange={() => toggle('tools', t)} />
                    <span className="mono">{t}</span>
                    <span className="hint" style={{ marginLeft: 'auto', color: isRisky ? '#f5a25d' : undefined }}>{isRisky ? 'writes' : 'read-only'}</span>
                  </label>
                );
              })}
            </div>
            {risky.length > 0 && (
              <div className="alert" role="alert">
                <Icon name="warn" size={18} />
                <span>{risky.join(', ')} runs as your macOS user: it can change files, reach the Realm socket and other bots’ data. Only grant it to bots whose charter and inputs you trust.</span>
              </div>
            )}
            <span className="hint">Unchecked tools are unavailable. Realm folders (~/.realm, .realm) are always blocked for file tools.</span>
          </fieldset>
        </div>
        <div style={{ flex: '2 1 320px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 18 }}>
          <fieldset>
            <legend>Skills</legend>
            <label className="check"><input type="checkbox" checked={d.skills.includes('*')} onChange={() => set('skills', d.skills.includes('*') ? [] : ['*'])} /> All skills at this scope</label>
            {!d.skills.includes('*') && (skills.data ?? []).map((s) => (
              <label key={s.source + s.name} className="check" title={s.description}>
                <input type="checkbox" checked={d.skills.includes(s.name)} onChange={() => toggle('skills', s.name)} />
                <span className="mono">{s.name}</span>
                <span className="hint" style={{ marginLeft: 'auto' }}>{s.source}</span>
              </label>
            ))}
            {skills.data?.length === 0 && <span className="hint">No skills found in ~/.claude/skills or the project’s .claude/skills.</span>}
          </fieldset>
          {!isEdit && (
            <fieldset>
              <legend>Start with knowledge from</legend>
              <label className="sr-only" htmlFor="bf-inherit">Inherit knowledge from bot</label>
              <select id="bf-inherit" className="input" value={d.inherit} onChange={(e) => set('inherit', e.target.value)}>
                <option value="">Nobody — start empty</option>
                {(sources.data ?? []).filter((b) => b.role !== 'moderator' && b.knowledge_count > 0).map((b) => (
                  <option key={b.handle} value={b.handle}>{b.handle} · {b.knowledge_count} notes{b.status === 'archived' ? ' (archived)' : ''}</option>
                ))}
              </select>
              <span className="hint">Notes are copied and marked with their origin. The source bot keeps its own.</span>
            </fieldset>
          )}
          {save.isError && <p className="error" role="alert">{String(save.error)}</p>}
          <div className="row" style={{ justifyContent: 'flex-end' }}>
            <button type="button" className="btn" onClick={() => history.back()}>Cancel</button>
            <button type="submit" className="btn primary" disabled={save.isPending}>{isEdit ? 'Save changes' : 'Create bot'}</button>
          </div>
        </div>
      </form>
    </main>
  );
}
