import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type Bot, type Status } from '../api';
import { GLOBAL } from '../logic';
import { NavLink } from '../components/Sidebar';
import { Icon } from '../components/Icon';

type Props = { scope: string };
type Filter = 'all' | Status;

const primaryAction = (b: Bot): { label: string; action: 'on' | 'off' | 'restore' } =>
  b.status === 'active' ? { label: 'Turn off', action: 'off' } : b.status === 'off' ? { label: 'Turn on', action: 'on' } : { label: 'Restore', action: 'restore' };

export function BotsPage({ scope }: Props) {
  const qc = useQueryClient();
  const bots = useQuery({ queryKey: ['bots', scope], queryFn: () => api.bots(scope) });
  const [filter, setFilter] = useState<Filter>('all');
  const [search, setSearch] = useState('');
  const status = useMutation({
    mutationFn: ({ handle, action }: { handle: string; action: 'on' | 'off' | 'archive' | 'restore' }) => api.setStatus(handle, action),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['bots'] }),
  });

  const all = (bots.data ?? []).filter((b) => b.role !== 'moderator');
  const count = (f: Filter) => all.filter((b) => f === 'all' || b.status === f).length;
  const q = search.toLowerCase();
  const shown = all.filter(
    (b) => (filter === 'all' || b.status === filter) && (!q || b.handle.includes(q) || b.model.includes(q) || b.topics.some((t) => t.includes(q))),
  );

  return (
    <main className="page">
      <div className="spread">
        <div>
          <h1>Bots</h1>
          <span className="sub">{scope === GLOBAL ? 'Global bots and every project’s bots' : `${scope} project bots and the global bots they can reach`}</span>
        </div>
        <div className="row">
          <label className="sr-only" htmlFor="bot-filter">Filter bots</label>
          <input id="bot-filter" className="input" style={{ width: 260 }} type="search" placeholder="Filter by name, topic or model" value={search} onChange={(e) => setSearch(e.target.value)} />
          <NavLink to={{ page: 'newBot', scope }} className="btn primary"><Icon name="plus" /> New bot</NavLink>
        </div>
      </div>
      <div className="row" role="group" aria-label="Status filter">
        {(['all', 'active', 'off', 'archived'] as Filter[]).map((f) => (
          <button key={f} type="button" className="choice" aria-pressed={filter === f} onClick={() => setFilter(f)}>
            {f[0].toUpperCase() + f.slice(1)} {count(f)}
          </button>
        ))}
      </div>
      {bots.isError && <p className="error">{String(bots.error)}</p>}
      {status.isError && <p className="error" role="alert">{String(status.error)}</p>}
      {bots.isSuccess && shown.length === 0 && (
        <p className="empty">No bots here yet. <NavLink to={{ page: 'newBot', scope }}>Create one</NavLink>.</p>
      )}
      {shown.length > 0 && (
        <div className="table-box">
          <table className="list">
            <thead>
              <tr><th scope="col">Bot</th><th scope="col">Status</th><th scope="col">Runtime</th><th scope="col">Charter</th><th scope="col">Knowledge</th><th scope="col" style={{ textAlign: 'right' }}>Actions</th></tr>
            </thead>
            <tbody>
              {shown.map((b) => {
                const p = primaryAction(b);
                return (
                  <tr key={b.handle} style={{ opacity: b.status === 'archived' ? 0.7 : 1 }}>
                    <td>
                      <NavLink to={{ page: 'bot', handle: b.handle }}><b style={{ color: 'var(--text)' }}>{b.handle}</b></NavLink>
                      <div className="sub">{b.topics.join(' · ')}</div>
                    </td>
                    <td><span className={`pill ${b.status}`}>● {b.status}</span></td>
                    <td><span className="mono">{b.model || 'default'}{b.effort ? ` · ${b.effort}` : ''}</span><div className="sub">{b.provider}</div></td>
                    <td style={{ maxWidth: 340 }}>{b.charter.split('\n')[0]}</td>
                    <td className="sub">{b.knowledge_count} notes</td>
                    <td>
                      <div className="row" style={{ justifyContent: 'flex-end' }}>
                        <button type="button" className="btn small" onClick={() => status.mutate({ handle: b.handle, action: p.action })}>{p.label}</button>
                        <NavLink to={{ page: 'newBot', scope: b.scope, edit: b.handle }} className="btn small">Edit</NavLink>
                        {b.status !== 'archived' && (
                          <button type="button" className="btn small danger" onClick={() => window.confirm(`Archive ${b.handle}? It will reject messages; knowledge is kept.`) && status.mutate({ handle: b.handle, action: 'archive' })}>Archive</button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </main>
  );
}
