import { useQuery } from '@tanstack/react-query';
import { api } from '../api';
import { NavLink } from '../components/Sidebar';

export function SettingsPage() {
  const projects = useQuery({ queryKey: ['projects'], queryFn: api.projects });
  const providers = useQuery({ queryKey: ['providers'], queryFn: api.providers });
  const settings = useQuery({ queryKey: ['settings'], queryFn: api.settings });
  const s = settings.data;
  return (
    <main className="page">
      <h1>Projects &amp; settings</h1>
      <div className="grid2" style={{ alignItems: 'start' }}>
        <section className="card" aria-labelledby="pj">
          <div className="spread">
            <h2 id="pj" style={{ margin: 0, fontSize: 16 }}>Projects</h2>
            <NavLink to={{ page: 'onboard' }} className="btn small">Add project</NavLink>
          </div>
          {projects.data?.length === 0 && <p className="sub">No projects yet.</p>}
          {(projects.data ?? []).map((p) => (
            <div key={p.slug} className="member">
              <b>{p.slug}</b>
              <span className="sub mono" style={{ overflowWrap: 'anywhere' }}>{p.root}</span>
            </div>
          ))}
        </section>
        <section className="card" aria-labelledby="pv">
          <h2 id="pv" style={{ margin: 0, fontSize: 16 }}>Runtimes</h2>
          <table className="list">
            <thead><tr><th scope="col">Name</th><th scope="col">Type</th><th scope="col">Status</th></tr></thead>
            <tbody>
              {(providers.data ?? []).map((p) => (
                <tr key={p.name}><td><b>{p.name}</b></td><td>{p.type}</td><td style={{ color: p.ready ? 'var(--ok)' : 'var(--ask)' }}>{p.note}</td></tr>
              ))}
            </tbody>
          </table>
          <span className="hint">Keys are read from the daemon’s environment and never shown or stored. Edit ~/.realm/config.yaml to add providers.</span>
        </section>
        <section className="card" aria-labelledby="lm">
          <h2 id="lm" style={{ margin: 0, fontSize: 16 }}>Moderator &amp; limits</h2>
          {s && (
            <dl className="sub" style={{ margin: 0, display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '8px 16px' }}>
              <dt>Moderator model</dt><dd className="mono" style={{ margin: 0 }}>{s.moderator_model}</dd>
              <dt>Concurrent turns</dt><dd style={{ margin: 0 }}>{s.limits.max_concurrent}</dd>
              <dt>Max delegation hops</dt><dd style={{ margin: 0 }}>{s.limits.max_hops}</dd>
              <dt>History per turn</dt><dd style={{ margin: 0 }}>{s.limits.history_messages} messages</dd>
              <dt>Turn timeout</dt><dd style={{ margin: 0 }}>{s.limits.turn_timeout_sec}s</dd>
              <dt>Safety caps</dt><dd style={{ margin: 0 }}>{s.max_fanout_per_turn} bot messages per turn · {s.max_turns_per_message} bot turns per user message</dd>
            </dl>
          )}
          <span className="hint">Change these in <span className="mono">{s?.home ?? '~/.realm'}/config.yaml</span>, then restart the daemon. Change a moderator with <span className="mono">realm bot set @moderator --model …</span>.</span>
        </section>
        <section className="card" aria-labelledby="mcp">
          <h2 id="mcp" style={{ margin: 0, fontSize: 16 }}>Manage Realm from Claude Code</h2>
          <p className="sub" style={{ margin: 0 }}>The realm-admin MCP server lets Claude Code or Claude Desktop create bots, start chats and read replies for you.</p>
          <pre className="mono" style={{ margin: 0, padding: '12px 14px', borderRadius: 8, background: 'var(--bg-sunk)', border: '1px solid var(--line)', overflowX: 'auto' }}>claude mcp add realm -- realm mcp-admin</pre>
          <span className="hint">Bots use a separate, scoped MCP server (realm mcp) that the daemon starts for each turn.</span>
        </section>
      </div>
    </main>
  );
}
