import { useEffect, useRef, useState } from 'react';
import type { Bot, Message } from '../api';
import { isAnswered, questionOptions, questionText, timeOf } from '../logic';
import { Icon } from './Icon';

type Props = {
  messages: Message[];
  bots: Bot[];
  onPick: (questionId: string, handle: string) => void;
  onReroute: (messageId: string, handle: string) => void;
};

export function MessageList({ messages, bots, onPick, onReroute }: Props) {
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: 'end' });
  }, [messages.length]);

  if (messages.length === 0) {
    return <section className="messages" aria-label="Messages"><p className="empty">No messages yet. Say hi, or @mention a bot.</p></section>;
  }
  return (
    <section className="messages" aria-label="Messages" aria-live="polite">
      {messages.map((m) => {
        if (m.kind === 'system') return <SystemLine key={m.id} m={m} />;
        if (m.kind === 'question') return <Question key={m.id} m={m} answered={isAnswered(m, messages)} onPick={onPick} />;
        return <Bubble key={m.id} m={m} bots={bots} onReroute={onReroute} />;
      })}
      <div ref={end} />
    </section>
  );
}

function SystemLine({ m }: { m: Message }) {
  const isRoute = m.text.startsWith('→');
  return (
    <div className="sys row">
      {isRoute && <Icon name="arrow" size={14} />}
      <span>{isRoute ? m.text.slice(1).trim() : m.text}</span>
    </div>
  );
}

function Question({ m, answered, onPick }: { m: Message; answered: boolean; onPick: Props['onPick'] }) {
  const options = questionOptions(m);
  return (
    <div className="question" role="note">
      <div className="q-head row"><Icon name="ask" /> {answered ? 'Moderator · answered' : 'Moderator · no bot has run yet'}</div>
      <p className="msg-text">{questionText(m)}</p>
      {!answered && (
        <div className="row">
          {options.map((h) => (
            <button key={h} type="button" className="btn small" onClick={() => onPick(m.id, h)}>{h}</button>
          ))}
        </div>
      )}
    </div>
  );
}

function Bubble({ m, bots, onReroute }: { m: Message; bots: Bot[]; onReroute: Props['onReroute'] }) {
  const [isRerouting, setRerouting] = useState(false);
  const isUser = m.from === '@user';
  const initial = isUser ? 'Y' : (m.from.split('/').pop() ?? '?').charAt(0).toUpperCase();
  const others = bots.filter((b) => b.status === 'active');
  return (
    <article className={`msg ${m.kind}`}>
      <div className={`avatar ${isUser ? '' : 'bot'}`} aria-hidden="true">{initial}</div>
      <div style={{ minWidth: 0, flex: 1 }}>
        <div className="msg-head">
          <b>{isUser ? 'You' : m.from}</b>
          {m.kind !== 'text' && m.kind !== 'reply' && <span className="chip">{m.kind}</span>}
          <span className="msg-time">{timeOf(m.ts)}</span>
          {isUser && others.length > 0 && (
            <span className="msg-actions">
              {isRerouting ? (
                <select
                  className="input"
                  aria-label="Reroute to bot"
                  defaultValue=""
                  onChange={(e) => { if (e.target.value) onReroute(m.id, e.target.value); setRerouting(false); }}
                >
                  <option value="" disabled>Reroute to…</option>
                  {others.map((b) => <option key={b.handle} value={b.handle}>{b.handle}</option>)}
                </select>
              ) : (
                <button type="button" className="btn small" onClick={() => setRerouting(true)}>Reroute</button>
              )}
            </span>
          )}
        </div>
        <p className="msg-text">{m.text}</p>
      </div>
    </article>
  );
}
