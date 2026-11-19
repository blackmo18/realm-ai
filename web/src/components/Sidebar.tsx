import type { MouseEvent, ReactNode } from 'react';
import { useQueries, useQuery } from '@tanstack/react-query';
import { api } from '../api';
import { GLOBAL } from '../logic';
import { href, navigate, type Route } from '../router';
import { Icon } from './Icon';

interface SidebarProps {
  route: Route;
}

/** Client-side navigation for in-app links. */
export function NavLink({ to, current, className, children, label }: { to: Route; current?: boolean; className?: string; children: ReactNode; label?: string }) {
  const onClick = (e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey) return;
    e.preventDefault();
    navigate(to);
  };
  return (
    <a href={href(to)} onClick={onClick} className={className} aria-current={current ? 'page' : undefined} aria-label={label}>
      {children}
    </a>
  );
}

export function Sidebar({ route }: SidebarProps) {
  const projects = useQuery({ queryKey: ['projects'], queryFn: api.projects });
  const scopes = [GLOBAL, ...(projects.data ?? []).map((p) => p.slug)];
  const chats = useQueries({
    queries: scopes.map((s) => ({ queryKey: ['chats', s], queryFn: () => api.chats(s) })),
  });
  const online = !projects.isError;

  return (
    <nav className="sidebar" aria-label="Realm navigation">
      <div className="brand">
        <b><Icon name="realm" /> Realm</b>
        <span className="sub row"><span className="dot" style={{ background: online ? 'var(--ok)' : 'var(--bad)' }} />daemon</span>
      </div>
      {scopes.map((scope, i) => {
        const groupChats = (chats[i]?.data ?? []).filter((c) => c.kind === 'group');
        const dms = (chats[i]?.data ?? []).filter((c) => c.kind === 'dm');
        return (
          <div className="nav-group" key={scope}>
            <div className="nav-head">
              <span>{scope === GLOBAL ? 'Global' : `Project · ${scope}`}</span>
              <NavLink to={{ page: 'newChat', scope }} className="icon-btn" label={`New chat in ${scope}`}><Icon name="plus" /></NavLink>
            </div>
            {groupChats.map((c) => (
              <NavLink key={c.id} to={{ page: 'chat', id: c.id }} className="nav-item" current={route.page === 'chat' && route.id === c.id}>
                <span># {c.name}</span>
              </NavLink>
            ))}
            <NavLink to={{ page: 'bots', scope }} className="nav-item" current={route.page === 'bots' && route.scope === scope}>
              <span>Bots</span>
            </NavLink>
            {dms.length > 0 && (
              <details>
                <summary className="nav-item">Direct messages <span className="sub">{dms.length}</span></summary>
                {dms.map((c) => (
                  <NavLink key={c.id} to={{ page: 'chat', id: c.id }} className="nav-item" current={route.page === 'chat' && route.id === c.id}>
                    <span>{c.name.replace('@user+', '').replace('+@user', '')}</span>
                  </NavLink>
                ))}
              </details>
            )}
          </div>
        );
      })}
      <div className="nav-foot">
        <NavLink to={{ page: 'onboard' }} className="nav-item" current={route.page === 'onboard'}>
          <span className="row"><Icon name="folder" /> Add project</span>
        </NavLink>
        <NavLink to={{ page: 'settings' }} className="nav-item" current={route.page === 'settings'}>
          <span>Projects &amp; settings</span>
        </NavLink>
      </div>
    </nav>
  );
}
