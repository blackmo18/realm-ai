import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type Message } from '../api';
import { canJoinChat, mergeMessages } from '../logic';
import { Composer } from '../components/Composer';
import { MessageList } from '../components/MessageList';

type Props = { chatId: string };

export function ChatRoom({ chatId }: Props) {
  const qc = useQueryClient();
  const chat = useQuery({ queryKey: ['chat', chatId], queryFn: () => api.chat(chatId) });
  const members = useQuery({ queryKey: ['members', chatId], queryFn: () => api.members(chatId), enabled: chat.data?.kind === 'group' });
  const allBots = useQuery({ queryKey: ['bots', chat.data?.scope], queryFn: () => api.bots(chat.data!.scope), enabled: !!chat.data });
  const [messages, setMessages] = useState<Message[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    let isLive = true;
    // Merge, never replace: stream messages may arrive before the history fetch resolves.
    const loadHistory = () =>
      api.messages(chatId)
        .then((history) => isLive && setMessages((cur) => history.reduce(mergeMessages, cur)))
        .catch((e: unknown) => isLive && setError(String(e)));
    setMessages([]);
    void loadHistory();
    const close = api.stream(
      chatId,
      (m) => setMessages((cur) => mergeMessages(cur, m)),
      (state) => {
        if (!isLive) return;
        if (state === 'open') {
          setError('');
          void loadHistory(); // catch up on anything missed while disconnected
        } else {
          setError('Live updates stopped. If the daemon restarted, reopen the UI with `realm ui`.');
        }
      },
    );
    return () => { isLive = false; close(); };
  }, [chatId]);

  const act = (fn: () => Promise<unknown>) => fn().then(() => setError('')).catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)));
  const send = useMutation({ mutationFn: (text: string) => api.post(chatId, text) });
  const routing = useMutation({
    mutationFn: (mode: string) => api.setRouting(chatId, mode),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['chat', chatId] }),
  });

  if (chat.isError) return <main className="page"><p className="error">{String(chat.error)}</p></main>;
  if (!chat.data) return <main className="page"><p className="sub">Loading…</p></main>;
  const c = chat.data;
  const scopeBots = (allBots.data ?? []).filter((b) => canJoinChat(b, c.scope));
  const isGroup = c.kind === 'group';
  const title = isGroup ? `# ${c.name}` : c.name.replace('@user+', '').replace('+@user', '');

  return (
    <>
      <header className="chat-head">
        <div>
          <h1>{title} <span className="sub">{c.scope}</span></h1>
          {isGroup && (
            <span className="sub">
              {c.routing === 'moderated'
                ? 'Messages without @ go to the moderator, which routes them to one bot or asks you.'
                : 'Only @mentions reach bots.'}
            </span>
          )}
        </div>
        {isGroup && (
          <div className="row">
            <span className="sub">Routing</span>
            <div className="seg" role="group" aria-label="Routing mode">
              <button type="button" aria-pressed={c.routing === 'moderated'} onClick={() => routing.mutate('moderated')}>Moderated</button>
              <button type="button" aria-pressed={c.routing === 'mention'} onClick={() => routing.mutate('mention')}>Mention only</button>
            </div>
          </div>
        )}
      </header>
      {error && <p className="error" role="alert" style={{ margin: '12px 24px 0' }}>{error}</p>}
      <div className="chat-body">
        <div className="chat-main">
          <MessageList
            messages={messages}
            bots={scopeBots}
            onPick={(ask, bot) => act(() => api.pick(chatId, bot, ask))}
            onReroute={(id, bot) => act(() => api.reroute(chatId, id, bot))}
          />
          <Composer
            label={`Message ${title}`}
            bots={scopeBots}
            isSending={send.isPending}
            onSend={(text) => act(() => send.mutateAsync(text))}
          />
        </div>
        {isGroup && <MembersPanel chatId={chatId} scope={c.scope} members={members.data ?? []} candidates={allBots.data ?? []} />}
      </div>
    </>
  );
}

type MembersProps = {
  chatId: string;
  scope: string;
  members: { handle: string; status: string; charter: string; role: string }[];
  candidates: { handle: string; scope: string; role: string; status: string }[];
};

function MembersPanel({ chatId, scope, members, candidates }: MembersProps) {
  const qc = useQueryClient();
  const [adding, setAdding] = useState(false);
  const addable = candidates.filter((b) => b.role !== 'moderator' && b.status !== 'archived' && canJoinChat(b, scope) && !members.some((m) => m.handle === b.handle));
  const add = useMutation({
    mutationFn: (handle: string) => api.addMember(chatId, handle),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['members', chatId] }),
  });
  return (
    <aside className="members" aria-label="Chat members">
      <div className="spread">
        <h2 className="nav-head" style={{ margin: 0 }}>Members</h2>
        {addable.length > 0 && <button type="button" className="btn small" onClick={() => setAdding((v) => !v)}>Add bot</button>}
      </div>
      {adding && (
        <select className="input" aria-label="Add a bot to this chat" defaultValue="" onChange={(e) => { if (e.target.value) add.mutate(e.target.value); setAdding(false); }}>
          <option value="" disabled>Choose a bot…</option>
          {addable.map((b) => <option key={b.handle} value={b.handle}>{b.handle}</option>)}
        </select>
      )}
      {members.map((m) => (
        <div className="member" key={m.handle}>
          <div className="spread"><b>{m.handle}</b><span className={`pill ${m.status}`}>● {m.status}</span></div>
          <span className="sub">{m.charter.split('\n')[0]}</span>
        </div>
      ))}
      <p className="hint">
        {scope === 'global'
          ? 'Global chats take global bots only.'
          : 'Every active project bot is a member. Global bots join when added or mentioned. Project bots never join global chats.'}
      </p>
    </aside>
  );
}
