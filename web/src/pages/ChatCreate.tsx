import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import { canJoinChat, GLOBAL, slugify } from '../logic';
import { navigate } from '../router';

type Props = { scope: string };

export function ChatCreate({ scope: initialScope }: Props) {
  const qc = useQueryClient();
  const [scope, setScope] = useState(initialScope);
  const [name, setName] = useState('');
  const [routing, setRouting] = useState<'moderated' | 'mention'>('moderated');
  const [extra, setExtra] = useState<string[]>([]);
  const projects = useQuery({ queryKey: ['projects'], queryFn: api.projects });
  const bots = useQuery({ queryKey: ['bots', GLOBAL], queryFn: () => api.bots(GLOBAL) });
  const create = useMutation({
    mutationFn: () => api.createChat(scope, slugify(name), routing, extra.filter((h) => h.startsWith('@global/'))),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: ['chats', scope] });
      navigate({ page: 'chat', id: c.id });
    },
  });
  const candidates = (bots.data ?? []).filter((b) => b.role !== 'moderator' && b.status !== 'archived');
  const submit = (e: FormEvent) => { e.preventDefault(); create.mutate(); };

  return (
    <main className="page" style={{ maxWidth: 760 }}>
      <form className="card" onSubmit={submit}>
        <div>
          <h1>New group chat</h1>
          <span className="sub">Chat id: <span className="mono">{scope}/{slugify(name) || 'name'}</span></span>
        </div>
        <div className="grid2">
          <div className="field">
            <label htmlFor="cc-name">Name</label>
            <input id="cc-name" className="input" required value={name} onChange={(e) => setName(e.target.value)} placeholder="release" />
          </div>
          <div className="field">
            <span className="label" id="cc-scope">Scope</span>
            <div className="seg" role="group" aria-labelledby="cc-scope">
              {[GLOBAL, ...(projects.data ?? []).map((p) => p.slug)].map((s) => (
                <button key={s} type="button" aria-pressed={scope === s} onClick={() => setScope(s)}>{s === GLOBAL ? 'Global' : s}</button>
              ))}
            </div>
          </div>
        </div>
        <fieldset>
          <legend>Routing</legend>
          <label className={`check ${routing === 'moderated' ? 'on' : ''}`}>
            <input type="radio" name="routing" checked={routing === 'moderated'} onChange={() => setRouting('moderated')} />
            <span><b>Moderated</b> <span className="sub">— messages without @ cost one small moderator call; it routes or asks you.</span></span>
          </label>
          <label className={`check ${routing === 'mention' ? 'on' : ''}`}>
            <input type="radio" name="routing" checked={routing === 'mention'} onChange={() => setRouting('mention')} />
            <span><b>Mention only</b> <span className="sub">— only @mentions reach bots. Zero model calls otherwise.</span></span>
          </label>
        </fieldset>
        <fieldset>
          <legend>Bots in this chat</legend>
          <span className="hint">
            {scope === GLOBAL ? 'Global chats take global bots only. Project bots can never join.' : `Every active ${scope} bot is in by default. Tick global bots to add them.`}
          </span>
          {candidates.map((b) => {
            const isBlocked = !canJoinChat(b, scope);
            const isOwnScope = b.scope === scope && scope !== GLOBAL;
            const isChecked = isOwnScope || extra.includes(b.handle);
            const note = isBlocked ? 'project bot · not allowed here' : isOwnScope ? 'project · included' : b.scope === GLOBAL ? 'global · optional' : '';
            return (
              <label key={b.handle} className={`check ${isBlocked ? 'blocked' : ''}`}>
                <input
                  type="checkbox"
                  checked={isChecked && !isBlocked}
                  disabled={isBlocked || isOwnScope}
                  onChange={() => setExtra((cur) => (cur.includes(b.handle) ? cur.filter((h) => h !== b.handle) : [...cur, b.handle]))}
                />
                <b>{b.handle}</b>
                <span className="hint" style={{ marginLeft: 'auto', color: isBlocked ? 'var(--bad)' : undefined }}>{note}</span>
              </label>
            );
          })}
        </fieldset>
        {create.isError && <p className="error" role="alert">{String(create.error)}</p>}
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button type="button" className="btn" onClick={() => history.back()}>Cancel</button>
          <button type="submit" className="btn primary" disabled={create.isPending || !slugify(name)}>Create chat</button>
        </div>
      </form>
    </main>
  );
}
