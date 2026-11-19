import { useState, type KeyboardEvent, type FormEvent } from 'react';
import type { Bot } from '../api';
import { applyMention, mentionQuery, mentionSuggestions } from '../logic';

type Props = {
  label: string;
  bots: Bot[];
  isSending: boolean;
  onSend: (text: string) => Promise<void>;
};

/** Floating, auto-growing message box with @ suggestions above it. */
export function Composer({ label, bots, isSending, onSend }: Props) {
  const [text, setText] = useState('');
  const [active, setActive] = useState(0);
  const query = mentionQuery(text);
  const suggestions = query === null ? [] : mentionSuggestions(bots, query);
  const isOpen = suggestions.length > 0;

  const choose = (handle: string) => {
    setText((t) => applyMention(t, handle));
    setActive(0);
  };

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    const value = text.trim();
    if (!value || isSending) return;
    await onSend(value);
    setText('');
  };

  const handleKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (isOpen && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault();
      const step = e.key === 'ArrowDown' ? 1 : -1;
      setActive((i) => (i + step + suggestions.length) % suggestions.length);
      return;
    }
    if (isOpen && (e.key === 'Tab' || e.key === 'Enter')) {
      e.preventDefault();
      choose(suggestions[Math.min(active, suggestions.length - 1)].handle);
      return;
    }
    if (isOpen && e.key === 'Escape') {
      setText((t) => t + ' ');
      return;
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <form className="composer" onSubmit={submit}>
      {isOpen && (
        <ul className="mentions" role="listbox" aria-label="Mention suggestions">
          {suggestions.map((b, i) => (
            <li key={b.handle} role="option" aria-selected={i === active} onMouseDown={(e) => { e.preventDefault(); choose(b.handle); }}>
              <span>{b.handle}</span>
              <span className="sub">{b.status === 'active' ? b.charter.split('\n')[0] : `${b.status} · messages queue`}</span>
            </li>
          ))}
        </ul>
      )}
      <label htmlFor="composer" className="sr-only">{label}</label>
      <textarea
        id="composer"
        rows={1}
        value={text}
        placeholder={`${label} — @mention a bot, or let the moderator route it · Shift+Enter for a new line`}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={handleKey}
        aria-autocomplete="list"
        aria-expanded={isOpen}
      />
      <button type="submit" className="btn primary" disabled={isSending || !text.trim()}>Send</button>
    </form>
  );
}
