import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type FolderInfo, type OnboardResult } from '../api';
import { GLOBAL, slugify } from '../logic';
import { navigate } from '../router';
import { Icon } from '../components/Icon';

const STEPS = ['1 · Folder', '2 · Starter bots', '3 · First chat'];

export function Onboard() {
  const qc = useQueryClient();
  const [step, setStep] = useState(0);
  const [folder, setFolder] = useState('');
  const [slug, setSlug] = useState('');
  const [ignoreDB, setIgnoreDB] = useState(true);
  const [starters, setStarters] = useState<string[]>(['reviewer']);
  const [globals, setGlobals] = useState<string[]>([]);
  const [chatName, setChatName] = useState('general');
  const [routing, setRouting] = useState<'moderated' | 'mention'>('moderated');
  const [done, setDone] = useState<OnboardResult | null>(null);

  const suggest = useQuery({ queryKey: ['suggest'], queryFn: api.suggest });
  const info = useQuery({ queryKey: ['inspect', folder], queryFn: () => api.inspect(folder), enabled: folder.trim().length > 1 });
  const templates = useQuery({ queryKey: ['templates'], queryFn: api.templates });
  const globalBots = useQuery({ queryKey: ['bots', GLOBAL], queryFn: () => api.bots(GLOBAL) });
  const onboard = useMutation({
    mutationFn: () => api.onboard({ dir: folder, slug: slug || undefined, ignore_db: ignoreDB, starters, global_members: globals, chat_name: slugify(chatName) || 'general', routing }),
    onSuccess: (r) => {
      setDone(r);
      void qc.invalidateQueries({ queryKey: ['projects'] });
    },
  });
  const project = slug || info.data?.slug || 'project';
  const canContinue = step !== 0 || (!!info.data?.exists && !info.data.registered);
  const toggle = (list: string[], set: (v: string[]) => void, v: string) => set(list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  const pickFolder = (p: string) => { setFolder(p); setSlug(''); };

  if (done) {
    return (
      <main className="page" style={{ maxWidth: 760 }}>
        <section className="card">
          <h1 className="row" style={{ color: 'var(--ok)' }}><Icon name="check" size={22} /> {done.project} is ready</h1>
          <ul className="sub">
            <li>Created <span className="mono">{done.root}/.realm</span></li>
            <li>{done.bots.length ? `Created ${done.bots.join(', ')}` : 'No starter bots — add some from Bots'}</li>
            <li>Opened <span className="mono">#{done.chat.name}</span> ({done.chat.routing})</li>
          </ul>
          <div><button type="button" className="btn primary" onClick={() => navigate({ page: 'chat', id: done.chat.id })}>Open #{done.chat.name}</button></div>
        </section>
      </main>
    );
  }

  return (
    <main className="page" style={{ maxWidth: 800 }}>
      <div>
        <h1>Add a project</h1>
        <span className="sub">Register a folder so it gets its own bots, knowledge and chats.</span>
      </div>
      <ol className="steps" aria-label="Steps">
        {STEPS.map((s, i) => <li key={s} className={i <= step ? 'done' : ''} aria-current={i === step ? 'step' : undefined}><span />{s}</li>)}
      </ol>
      <section className="card">
        {step === 0 && (
          <FolderStep folder={folder} onFolder={pickFolder} suggestions={suggest.data ?? []} info={info.data} slug={slug || info.data?.slug || ''} onSlug={setSlug} ignoreDB={ignoreDB} onIgnore={setIgnoreDB} />
        )}
        {step === 1 && (
          <>
            <div><h2 style={{ margin: 0, fontSize: 16 }}>Starter bots</h2><span className="sub">Pick a few to begin with; edit their charters any time. You can skip this.</span></div>
            {(templates.data ?? []).map((t) => (
              <label key={t.name} className={`check ${starters.includes(t.name) ? 'on' : ''}`} style={{ alignItems: 'flex-start', padding: 12 }}>
                <input type="checkbox" checked={starters.includes(t.name)} onChange={() => toggle(starters, setStarters, t.name)} />
                <span style={{ flex: 1 }}>
                  <span className="spread"><b>@{project}/{t.name}</b><span className="sub mono">{t.model}{t.effort ? ` · ${t.effort}` : ''}</span></span>
                  <span className="sub">{t.charter}</span>
                </span>
              </label>
            ))}
            <span className="label">Global bots to bring into the first chat</span>
            {(globalBots.data ?? []).filter((b) => b.scope === GLOBAL && b.role !== 'moderator' && b.status === 'active').map((b) => (
              <label key={b.handle} className="check">
                <input type="checkbox" checked={globals.includes(b.handle)} onChange={() => toggle(globals, setGlobals, b.handle)} />
                <span>{b.handle}</span><span className="hint">{b.charter.split('\n')[0]}</span>
              </label>
            ))}
          </>
        )}
        {step === 2 && (
          <>
            <div><h2 style={{ margin: 0, fontSize: 16 }}>First group chat</h2><span className="sub">Where you talk to this project’s bots together.</span></div>
            <div className="field">
              <label htmlFor="ob-chat">Chat name</label>
              <input id="ob-chat" className="input" value={chatName} onChange={(e) => setChatName(e.target.value)} />
              <span className="hint">Chat id: <span className="mono">{project}/{slugify(chatName) || 'general'}</span></span>
            </div>
            <div className="seg" role="group" aria-label="Routing">
              <button type="button" aria-pressed={routing === 'moderated'} onClick={() => setRouting('moderated')}>Moderated</button>
              <button type="button" aria-pressed={routing === 'mention'} onClick={() => setRouting('mention')}>Mention only</button>
            </div>
            <div className="found"><span className="nav-head" style={{ padding: 0 }}>Members</span>
              <span>{[...starters.map((s) => `@${project}/${s}`), ...globals].join(', ') || 'No bots yet'}</span>
            </div>
          </>
        )}
      </section>
      {onboard.isError && <p className="error" role="alert">{String(onboard.error)}</p>}
      <div className="spread">
        <button type="button" className="btn" onClick={() => (step === 0 ? history.back() : setStep(step - 1))}>{step === 0 ? 'Cancel' : 'Back'}</button>
        <div className="row">
          {step === 1 && <button type="button" className="btn" onClick={() => { setStarters([]); setStep(2); }}>Skip</button>}
          {step < 2 && <button type="button" className="btn primary" disabled={!canContinue} onClick={() => setStep(step + 1)}>Continue</button>}
          {step === 2 && <button type="button" className="btn primary" disabled={onboard.isPending} onClick={() => onboard.mutate()}>Create project</button>}
        </div>
      </div>
    </main>
  );
}

type FolderStepProps = {
  folder: string;
  onFolder: (v: string) => void;
  suggestions: string[];
  info?: FolderInfo;
  slug: string;
  onSlug: (v: string) => void;
  ignoreDB: boolean;
  onIgnore: (v: boolean) => void;
};

function FolderStep({ folder, onFolder, suggestions, info, slug, onSlug, ignoreDB, onIgnore }: FolderStepProps) {
  return (
    <>
      <div className="field">
        <label htmlFor="ob-folder">Project folder</label>
        <input id="ob-folder" className="input mono" placeholder="~/code/my-app" value={folder} onChange={(e) => onFolder(e.target.value)} />
      </div>
      {suggestions.length > 0 && (
        <div className="field">
          <span className="hint">Recent git folders not yet in Realm</span>
          <div className="row">{suggestions.map((p) => <button key={p} type="button" className="choice" aria-pressed={p === folder} onClick={() => onFolder(p)}>{p}</button>)}</div>
        </div>
      )}
      {info && (
        <div className="found" aria-live="polite">
          <span className="nav-head" style={{ padding: 0 }}>What Realm found</span>
          {!info.exists && <span style={{ color: 'var(--bad)' }}>✕ Folder not found</span>}
          {info.exists && (
            <>
              <span>{info.is_git ? '✓ Git repository' : '· Not a git repository'}</span>
              <span>{info.has_claude_md ? '✓ CLAUDE.md found — project bots read it on every turn' : '· No CLAUDE.md'}</span>
              <span>{info.skill_count ? `✓ ${info.skill_count} project skills in .claude/skills` : '· No project skills'}</span>
              <span style={{ color: info.registered ? 'var(--bad)' : undefined }}>{info.registered ? `✕ Already registered as ${info.slug}` : '· Not registered with Realm yet'}</span>
            </>
          )}
        </div>
      )}
      <div className="grid2">
        <div className="field">
          <label htmlFor="ob-slug">Project name</label>
          <input id="ob-slug" className="input mono" value={slug} onChange={(e) => onSlug(slugify(e.target.value))} />
          <span className="hint">Bots here: <span className="mono">@{slug || 'name'}/&lt;bot&gt;</span></span>
        </div>
        <label className="check" style={{ alignSelf: 'end' }}>
          <input type="checkbox" checked={ignoreDB} onChange={(e) => onIgnore(e.target.checked)} />
          <span>Keep chat history out of git <span className="hint">(adds .realm/realm.db to .gitignore)</span></span>
        </label>
      </div>
    </>
  );
}
