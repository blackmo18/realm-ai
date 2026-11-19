import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import type { Bot, Message } from '../api';
import { Composer } from './Composer';
import { MessageList } from './MessageList';
import { ChatCreate } from '../pages/ChatCreate';
import { BotForm } from '../pages/BotForm';

const bot = (handle: string, extra: Partial<Bot> = {}): Bot => ({
  handle, name: handle.split('/').pop()!, scope: handle.slice(1).split('/')[0], role: 'bot', status: 'active',
  provider: 'claude-cli', model: '', effort: '', charter: 'Owns things', topics: [], skills: [], tools: [], lineage: [],
  knowledge_count: 0, dir: '', ...extra,
});

function withClient(ui: ReactNode, responses: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const path = String(url).replace('/api', '').split('?')[0];
    const data = responses[path] ?? [];
    return { json: async () => ({ ok: true, data }), status: 200 } as Response;
  }));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

afterEach(() => vi.unstubAllGlobals());

describe('Composer', () => {
  test('shows @ suggestions above the box and inserts the picked handle', async () => {
    const user = userEvent.setup();
    const onSend = vi.fn().mockResolvedValue(undefined);
    render(<Composer label="Message #dev" bots={[bot('@app/db'), bot('@app/docs', { status: 'off' })]} isSending={false} onSend={onSend} />);
    const box = screen.getByLabelText('Message #dev');
    await user.type(box, 'ask @d');
    const list = screen.getByRole('listbox', { name: 'Mention suggestions' });
    expect(within(list).getAllByRole('option')).toHaveLength(2);
    await user.keyboard('{Enter}');
    expect(box).toHaveValue('ask @app/db ');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    await user.type(box, 'please{Shift>}{Enter}{/Shift}thanks');
    expect(onSend).not.toHaveBeenCalled();
    await user.keyboard('{Enter}');
    expect(onSend).toHaveBeenCalledWith('ask @app/db please\nthanks');
  });
});

describe('MessageList', () => {
  test('moderator question offers pick buttons until answered', async () => {
    const user = userEvent.setup();
    const onPick = vi.fn();
    const q: Message = { id: '02', chat: 'c', thread: '01', parent: '01', from: '@app/moderator', kind: 'question', hop: 0, ts: '', text: 'Code or schema? Options: @app/reviewer, @app/db. Reply with one handle, e.g. @app/reviewer.' };
    const src: Message = { id: '01', chat: 'c', thread: '01', from: '@user', kind: 'text', hop: 0, ts: '', text: 'look at users' };
    const { rerender } = render(<MessageList messages={[src, q]} bots={[]} onPick={onPick} onReroute={vi.fn()} />);
    expect(screen.getByText('Moderator · no bot has run yet')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '@app/db' }));
    expect(onPick).toHaveBeenCalledWith('02', '@app/db');
    const routed: Message = { id: '03', chat: 'c', thread: '01', parent: '01', from: '@realm', kind: 'system', hop: 0, ts: '', text: '→ @app/db (picked by user)' };
    rerender(<MessageList messages={[src, q, routed]} bots={[]} onPick={onPick} onReroute={vi.fn()} />);
    expect(screen.queryByRole('button', { name: '@app/db' })).not.toBeInTheDocument();
    expect(screen.getByText('@app/db (picked by user)')).toBeInTheDocument();
  });
});

describe('ChatCreate', () => {
  test('global chats block project bots; project chats include their own bots', async () => {
    const user = userEvent.setup();
    withClient(<ChatCreate scope="app" />, {
      '/v1/projects': [{ slug: 'app', root: '/code/app' }],
      '/v1/bots': [bot('@app/db'), bot('@global/ops')],
    });
    const db = await screen.findByRole('checkbox', { name: /@app\/db/ });
    expect(db).toBeChecked();
    expect(db).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Global' }));
    expect(screen.getByRole('checkbox', { name: /@app\/db/ })).not.toBeChecked();
    expect(screen.getByText('project bot · not allowed here')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /@global\/ops/ })).toBeEnabled();
  });
});

describe('BotForm', () => {
  test('warns when a write-capable tool is granted', async () => {
    const user = userEvent.setup();
    withClient(<BotForm scope="app" />, { '/v1/projects': [{ slug: 'app', root: '/code/app' }] });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    await user.click(screen.getByRole('checkbox', { name: /Bash/ }));
    expect(screen.getByRole('alert')).toHaveTextContent('Bash runs as your macOS user');
    expect(screen.getByText('@app/name')).toBeInTheDocument();
  });
});
