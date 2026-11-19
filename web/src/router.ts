// Minimal history-based router; the daemon serves index.html for any path.
import { useEffect, useState } from 'react';

export type Route =
  | { page: 'home' }
  | { page: 'chat'; id: string }
  | { page: 'bots'; scope: string }
  | { page: 'bot'; handle: string }
  | { page: 'newBot'; scope: string; edit?: string }
  | { page: 'newChat'; scope: string }
  | { page: 'onboard' }
  | { page: 'settings' };

export function parse(path: string, search: string): Route {
  const q = new URLSearchParams(search);
  const scope = q.get('scope') ?? 'global';
  switch (path) {
    case '/chat':
      return q.get('id') ? { page: 'chat', id: q.get('id')! } : { page: 'home' };
    case '/bots':
      return { page: 'bots', scope };
    case '/bot':
      return q.get('handle') ? { page: 'bot', handle: q.get('handle')! } : { page: 'home' };
    case '/bots/new':
      return { page: 'newBot', scope, edit: q.get('edit') ?? undefined };
    case '/chats/new':
      return { page: 'newChat', scope };
    case '/projects/new':
      return { page: 'onboard' };
    case '/settings':
      return { page: 'settings' };
    default:
      return { page: 'home' };
  }
}

export function href(r: Route): string {
  switch (r.page) {
    case 'chat':
      return '/chat?id=' + encodeURIComponent(r.id);
    case 'bots':
      return '/bots?scope=' + encodeURIComponent(r.scope);
    case 'bot':
      return '/bot?handle=' + encodeURIComponent(r.handle);
    case 'newBot':
      return '/bots/new?scope=' + encodeURIComponent(r.scope) + (r.edit ? '&edit=' + encodeURIComponent(r.edit) : '');
    case 'newChat':
      return '/chats/new?scope=' + encodeURIComponent(r.scope);
    case 'onboard':
      return '/projects/new';
    case 'settings':
      return '/settings';
    default:
      return '/';
  }
}

export function navigate(r: Route): void {
  history.pushState(null, '', href(r));
  window.dispatchEvent(new PopStateEvent('popstate'));
}

export function useRoute(): Route {
  const read = () => parse(window.location.pathname, window.location.search);
  const [route, setRoute] = useState<Route>(read);
  useEffect(() => {
    const on = () => setRoute(read());
    window.addEventListener('popstate', on);
    return () => window.removeEventListener('popstate', on);
  }, []);
  return route;
}
