import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type Entry } from '../api';
import { NavLink } from '../components/Sidebar';
import { navigate } from '../router';

type Props = { handle: string };

export function BotDetail({ handle }: Props) {
  const qc = useQueryClient();
  const bot = useQuery({ queryKey: ['bot', handle], queryFn: () => api.bot(handle) });
  const notes = useQuery({ queryKey: ['knowledge', handle], queryFn: () => api.knowledge(handle) });
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['bot', handle] });
    void qc.invalidateQueries({ queryKey: ['knowledge', handle] });
    void qc.invalidateQueries({ queryKey: ['bots'] });
  };
  const status = useMutation({ mutationFn: (a: 'on' | 'off' | 'archive' | 'restore') => api.setStatus(handle, a), onSuccess: refresh });
  const [editing, setEditing] = useState<Entry | 'new' | null>(null);

  if (bot.isError) return <main className="page"><p className="error">{String(bot.error)}</p></main>;
  if (!bot.data) return <main className="page"><p className="sub">Loading…</p></main>;
  const b = bot.data;

  return (
    <main className="page">
      <div className="spread">
        <div>
          <NavLink to={{ page: 'bots', scope: b.scope }}>← Bots</NavLink>
          <div className="row">
            <h1>{b.handle}</h1>
            <span className={`pill ${b.status}`}>● {b.status}</span>
            <span className="sub mono">{b.provider} · {b.model || 'default'}{b.effort ? ` · ${b.effort}` : ''}</span>
          </div>
        </div>
        <div className="row">
          {b.status === 'active' && <button type="button" className="btn" onClick={() => status.mutate('off')}>Turn off</button>}
          {b.status === 'off' && <button type="button" className="btn" onClick={() => status.mutate('on')}>Turn on</button>}
          {b.status === 'archived' && <button type="button" className="btn" onClick={() => status.mutate('restore')}>Restore</button>}
          {b.status !== 'archived' && <button type="button" className="btn danger" onClick={() => window.confirm(`Archive ${b.handle}?`) && status.mutate('archive')}>Archive</button>}
          <NavLink to={{ page: 'newBot', scope: b.scope, edit: b.handle }} className="btn primary">Edit bot</NavLink>
        </div>
      </div>
      {status.isError && <p className="error" role="alert">{String(status.error)}</p>}
      <div className="row" style={{ alignItems: 'flex-start', gap: 20 }}>
        <section style={{ flex: '3 1 520px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12 }} aria-labelledby="kn">
          <div className="spread">
            <h2 id="kn" style={{ margin: 0, fontSize: 16 }}>Knowledge <span className="sub">{notes.data?.length ?? 0} notes · added to every turn as an index</span></h2>
            <button type="button" className="btn small" onClick={() => setEditing('new')}>Add note</button>
          </div>
          {editing && <NoteEditor handle={handle} entry={editing === 'new' ? undefined : editing} onDone={() => { setEditing(null); refresh(); }} />}
          {notes.data?.length === 0 && !editing && <p className="empty">No notes yet. The bot saves facts with its remember tool, or add one yourself.</p>}
          {(notes.data ?? []).map((n) => (
            <article className="card" key={n.id}>
              <div className="spread"><b>{n.title}</b><span className="sub">{n.origin ? `from ${n.origin}` : 'saved'} · {n.created.slice(0, 10)}</span></div>
              <p className="msg-text">{n.body}</p>
              <div className="row">
                <button type="button" className="btn small" onClick={() => setEditing(n)}>Edit</button>
                <button type="button" className="btn small danger" onClick={() => window.confirm(`Forget "${n.title}"? This cannot be undone.`) && api.forgetKnowledge(handle, n.id).then(refresh)}>Forget</button>
              </div>
            </article>
          ))}
        </section>
        <div style={{ flex: '2 1 340px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <section className="card" aria-labelledby="ch">
            <h2 id="ch" className="nav-head" style={{ margin: 0 }}>Charter</h2>
            <p className="msg-text">{b.charter}</p>
            <div className="row">{b.topics.map((t) => <span key={t} className="chip">{t}</span>)}</div>
            <dl className="sub" style={{ margin: 0, display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '6px 12px' }}>
              <dt>Tools</dt><dd className="mono" style={{ margin: 0 }}>{b.tools.join(', ') || 'none'}</dd>
              <dt>Skills</dt><dd className="mono" style={{ margin: 0 }}>{b.skills.join(', ') || 'none'}</dd>
              <dt>Folder</dt><dd className="mono" style={{ margin: 0, overflowWrap: 'anywhere' }}>{b.dir}</dd>
            </dl>
          </section>
          <Lineage handle={handle} scope={b.scope} lineage={b.lineage} onDone={refresh} />
          <DirectMessage handle={b.handle} scope={b.scope} />
        </div>
      </div>
    </main>
  );
}

function NoteEditor({ handle, entry, onDone }: { handle: string; entry?: Entry; onDone: () => void }) {
  const [title, setTitle] = useState(entry?.title ?? '');
  const [body, setBody] = useState(entry?.body ?? '');
  const save = useMutation({
    mutationFn: () => (entry ? api.updateKnowledge(handle, entry.id, title, body) : api.addKnowledge(handle, title, body)),
    onSuccess: onDone,
  });
  const submit = (e: FormEvent) => { e.preventDefault(); save.mutate(); };
  return (
    <form className="card" onSubmit={submit}>
      <div className="field"><label htmlFor="note-title">Title</label><input id="note-title" className="input" required value={title} onChange={(e) => setTitle(e.target.value)} /></div>
      <div className="field"><label htmlFor="note-body">Note</label><textarea id="note-body" className="input" required value={body} onChange={(e) => setBody(e.target.value)} /></div>
      {save.isError && <p className="error">{String(save.error)}</p>}
      <div className="row" style={{ justifyContent: 'flex-end' }}>
        <button type="button" className="btn" onClick={onDone}>Cancel</button>
        <button type="submit" className="btn primary" disabled={save.isPending}>Save note</button>
      </div>
    </form>
  );
}

function Lineage({ handle, scope, lineage, onDone }: { handle: string; scope: string; lineage: { inherited_from: string; at: string }[]; onDone: () => void }) {
  const [isPicking, setPicking] = useState(false);
  const sources = useQuery({ queryKey: ['bots', scope], queryFn: () => api.bots(scope), enabled: isPicking });
  const inherit = useMutation({ mutationFn: (from: string) => api.inherit(handle, from), onSuccess: () => { setPicking(false); onDone(); } });
  return (
    <section className="card" aria-labelledby="ln">
      <h2 id="ln" className="nav-head" style={{ margin: 0 }}>Lineage</h2>
      {lineage.length === 0 && <span className="sub">Started empty.</span>}
      {lineage.map((l) => <span key={l.inherited_from + l.at} className="sub">Inherited from <b>{l.inherited_from}</b> on {l.at}.</span>)}
      {isPicking ? (
        <select className="input" aria-label="Inherit knowledge from" defaultValue="" onChange={(e) => e.target.value && inherit.mutate(e.target.value)}>
          <option value="" disabled>Choose a bot…</option>
          {(sources.data ?? []).filter((b) => b.handle !== handle && b.role !== 'moderator').map((b) => <option key={b.handle} value={b.handle}>{b.handle} · {b.knowledge_count} notes</option>)}
        </select>
      ) : (
        <button type="button" className="btn small" onClick={() => setPicking(true)}>Inherit from another bot…</button>
      )}
      {inherit.isError && <p className="error">{String(inherit.error)}</p>}
    </section>
  );
}

function DirectMessage({ handle, scope }: { handle: string; scope: string }) {
  const [text, setText] = useState('');
  const send = useMutation({
    mutationFn: () => api.dm(handle, scope, text),
    onSuccess: (m) => navigate({ page: 'chat', id: m.chat }),
  });
  return (
    <section className="card" aria-labelledby="dm">
      <h2 id="dm" className="nav-head" style={{ margin: 0 }}>Direct message</h2>
      <form className="row" onSubmit={(e) => { e.preventDefault(); if (text.trim()) send.mutate(); }}>
        <label htmlFor="dm-text" className="sr-only">Message {handle}</label>
        <input id="dm-text" className="input" style={{ flex: 1 }} placeholder={`Message ${handle}`} value={text} onChange={(e) => setText(e.target.value)} />
        <button type="submit" className="btn primary" disabled={send.isPending || !text.trim()}>Send</button>
      </form>
      {send.isError && <p className="error">{String(send.error)}</p>}
    </section>
  );
}
