// Package store persists projects, chats, messages, sessions and queued work
// in one SQLite database per scope.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// Chat kinds and routing modes.
const (
	KindGroup = "group"
	KindDM    = "dm"

	RoutingModerated = "moderated"
	RoutingMention   = "mention"
)

// Message kinds.
const (
	MsgText     = "text"
	MsgCommand  = "command"
	MsgInquiry  = "inquiry"
	MsgReply    = "reply"
	MsgRefusal  = "refusal"
	MsgSystem   = "system"
	MsgQuestion = "question"
)

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("not found")

// Chat is a group room or a DM thread.
type Chat struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Scope   string    `json:"scope"`
	Name    string    `json:"name"`
	Routing string    `json:"routing"`
	Created time.Time `json:"created"`
}

// Message is the wire and storage envelope.
type Message struct {
	ID       string    `json:"id"`
	Chat     string    `json:"chat"`
	Thread   string    `json:"thread"`
	Parent   string    `json:"parent,omitempty"`
	From     string    `json:"from"`
	Kind     string    `json:"kind"`
	Mentions []string  `json:"mentions,omitempty"`
	Hop      int       `json:"hop"`
	Text     string    `json:"text"`
	TS       time.Time `json:"ts"`
}

// Project is a registered project scope.
type Project struct {
	Slug string `json:"slug"`
	Root string `json:"root"`
}

// Store wraps one scope database.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects(slug TEXT PRIMARY KEY, root TEXT NOT NULL UNIQUE, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS chats(id TEXT PRIMARY KEY, kind TEXT NOT NULL, scope TEXT NOT NULL, name TEXT NOT NULL,
  routing TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS chat_members(chat_id TEXT NOT NULL, member TEXT NOT NULL, PRIMARY KEY(chat_id, member));
CREATE TABLE IF NOT EXISTS messages(id TEXT PRIMARY KEY, chat_id TEXT NOT NULL, thread TEXT NOT NULL, parent TEXT NOT NULL,
  sender TEXT NOT NULL, kind TEXT NOT NULL, mentions TEXT NOT NULL, hop INTEGER NOT NULL, body TEXT NOT NULL, ts TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS messages_chat ON messages(chat_id, id);
CREATE TABLE IF NOT EXISTS sessions(bot TEXT NOT NULL, chat_id TEXT NOT NULL, session_id TEXT NOT NULL, PRIMARY KEY(bot, chat_id));
CREATE TABLE IF NOT EXISTS inbox(id INTEGER PRIMARY KEY AUTOINCREMENT, bot TEXT NOT NULL, payload TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS asks(message_id TEXT PRIMARY KEY, chat_id TEXT NOT NULL, source_id TEXT NOT NULL, options TEXT NOT NULL);
`

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db folder: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

// RegisterProject records a project scope. Re-registering the same root
// returns the existing slug.
func (s *Store) RegisterProject(slug, root string) (Project, error) {
	var existing string
	err := s.db.QueryRow(`SELECT slug FROM projects WHERE root = ?`, root).Scan(&existing)
	if err == nil {
		return Project{Slug: existing, Root: root}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, err
	}
	candidate := slug
	for i := 2; ; i++ {
		_, err := s.db.Exec(`INSERT INTO projects(slug, root, created) VALUES(?,?,?)`, candidate, root, now())
		if err == nil {
			return Project{Slug: candidate, Root: root}, nil
		}
		if _, getErr := s.ProjectBySlug(candidate); getErr != nil {
			return Project{}, fmt.Errorf("register project: %w", err)
		}
		candidate = fmt.Sprintf("%s-%d", slug, i)
	}
}

// ProjectBySlug looks up a project.
func (s *Store) ProjectBySlug(slug string) (Project, error) {
	p := Project{Slug: slug}
	err := s.db.QueryRow(`SELECT root FROM projects WHERE slug = ?`, slug).Scan(&p.Root)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("project %q: %w", slug, ErrNotFound)
	}
	return p, err
}

// ProjectByRoot looks up a project by folder.
func (s *Store) ProjectByRoot(root string) (Project, error) {
	p := Project{Root: root}
	err := s.db.QueryRow(`SELECT slug FROM projects WHERE root = ?`, root).Scan(&p.Slug)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("project at %s: %w", root, ErrNotFound)
	}
	return p, err
}

// Projects lists registered projects.
func (s *Store) Projects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT slug, root FROM projects ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.Slug, &p.Root); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateChat inserts a chat; it fails if the id exists.
func (s *Store) CreateChat(c Chat) (Chat, error) {
	out := c
	out.Created = time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO chats(id, kind, scope, name, routing, created) VALUES(?,?,?,?,?,?)`,
		out.ID, out.Kind, out.Scope, out.Name, out.Routing, out.Created.Format(time.RFC3339Nano))
	if err != nil {
		return Chat{}, fmt.Errorf("create chat %s: %w", c.ID, err)
	}
	return out, nil
}

// GetChat loads a chat.
func (s *Store) GetChat(id string) (Chat, error) {
	var c Chat
	var created string
	err := s.db.QueryRow(`SELECT id, kind, scope, name, routing, created FROM chats WHERE id = ?`, id).
		Scan(&c.ID, &c.Kind, &c.Scope, &c.Name, &c.Routing, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, fmt.Errorf("chat %s: %w", id, ErrNotFound)
	}
	c.Created = parseTime(created)
	return c, err
}

// ListChats lists chats of a kind (empty kind = all).
func (s *Store) ListChats(kind string) ([]Chat, error) {
	rows, err := s.db.Query(`SELECT id, kind, scope, name, routing, created FROM chats WHERE ? = '' OR kind = ? ORDER BY id`, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var created string
		if err := rows.Scan(&c.ID, &c.Kind, &c.Scope, &c.Name, &c.Routing, &created); err != nil {
			return nil, err
		}
		c.Created = parseTime(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetRouting changes a chat's routing mode.
func (s *Store) SetRouting(id, routing string) error {
	res, err := s.db.Exec(`UPDATE chats SET routing = ? WHERE id = ?`, routing, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chat %s: %w", id, ErrNotFound)
	}
	return nil
}

// AddMember adds a member handle to a chat (idempotent).
func (s *Store) AddMember(chatID, member string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO chat_members(chat_id, member) VALUES(?,?)`, chatID, member)
	return err
}

// RemoveMember removes a member handle.
func (s *Store) RemoveMember(chatID, member string) error {
	_, err := s.db.Exec(`DELETE FROM chat_members WHERE chat_id = ? AND member = ?`, chatID, member)
	return err
}

// Members lists explicit member handles of a chat.
func (s *Store) Members(chatID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT member FROM chat_members WHERE chat_id = ? ORDER BY member`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertMessage stores a message.
func (s *Store) InsertMessage(m Message) error {
	mentions, err := json.Marshal(m.Mentions)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO messages(id, chat_id, thread, parent, sender, kind, mentions, hop, body, ts) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Chat, m.Thread, m.Parent, m.From, m.Kind, string(mentions), m.Hop, m.Text, m.TS.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

const msgCols = `id, chat_id, thread, parent, sender, kind, mentions, hop, body, ts`

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var mentions, ts string
		if err := rows.Scan(&m.ID, &m.Chat, &m.Thread, &m.Parent, &m.From, &m.Kind, &mentions, &m.Hop, &m.Text, &ts); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(mentions), &m.Mentions)
		m.TS = parseTime(ts)
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMessage loads one message.
func (s *Store) GetMessage(id string) (Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id)
	if err != nil {
		return Message{}, err
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, fmt.Errorf("message %s: %w", id, ErrNotFound)
	}
	return msgs[0], nil
}

// Recent returns up to limit latest messages of a chat, oldest first.
func (s *Store) Recent(chatID string, limit int) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM (SELECT * FROM messages WHERE chat_id = ? ORDER BY id DESC LIMIT ?) ORDER BY id`, chatID, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// Session returns the stored CLI session id for a bot in a chat.
func (s *Store) Session(bot, chatID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT session_id FROM sessions WHERE bot = ? AND chat_id = ?`, bot, chatID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// SetSession stores a CLI session id.
func (s *Store) SetSession(bot, chatID, sessionID string) error {
	_, err := s.db.Exec(`INSERT INTO sessions(bot, chat_id, session_id) VALUES(?,?,?)
		ON CONFLICT(bot, chat_id) DO UPDATE SET session_id = excluded.session_id`, bot, chatID, sessionID)
	return err
}

// PushInbox queues work for a bot that is off.
func (s *Store) PushInbox(bot string, payload []byte) error {
	_, err := s.db.Exec(`INSERT INTO inbox(bot, payload, created) VALUES(?,?,?)`, bot, string(payload), now())
	return err
}

// DrainInbox removes and returns queued work for a bot, oldest first.
func (s *Store) DrainInbox(bot string) ([][]byte, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`SELECT payload FROM inbox WHERE bot = ? ORDER BY id`, bot)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, []byte(p))
	}
	rows.Close()
	if _, err := tx.Exec(`DELETE FROM inbox WHERE bot = ?`, bot); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// Ask is a pending moderator question waiting for the user to pick a bot.
type Ask struct {
	MessageID string
	ChatID    string
	SourceID  string
	Options   []string
}

// SaveAsk records a pending moderator question.
func (s *Store) SaveAsk(a Ask) error {
	opts, err := json.Marshal(a.Options)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO asks(message_id, chat_id, source_id, options) VALUES(?,?,?,?)`,
		a.MessageID, a.ChatID, a.SourceID, string(opts))
	return err
}

// LatestAsk returns the newest pending ask in a chat.
func (s *Store) LatestAsk(chatID string) (Ask, error) {
	return s.queryAsk(`SELECT message_id, chat_id, source_id, options FROM asks WHERE chat_id = ? ORDER BY message_id DESC LIMIT 1`, chatID)
}

// GetAsk returns a pending ask by its question message id.
func (s *Store) GetAsk(messageID string) (Ask, error) {
	return s.queryAsk(`SELECT message_id, chat_id, source_id, options FROM asks WHERE message_id = ?`, messageID)
}

func (s *Store) queryAsk(q string, arg string) (Ask, error) {
	var a Ask
	var opts string
	err := s.db.QueryRow(q, arg).Scan(&a.MessageID, &a.ChatID, &a.SourceID, &opts)
	if errors.Is(err, sql.ErrNoRows) {
		return Ask{}, fmt.Errorf("pending question: %w", ErrNotFound)
	}
	if err != nil {
		return Ask{}, err
	}
	_ = json.Unmarshal([]byte(opts), &a.Options)
	return a, nil
}

// DeleteAsk removes a pending ask.
func (s *Store) DeleteAsk(messageID string) error {
	_, err := s.db.Exec(`DELETE FROM asks WHERE message_id = ?`, messageID)
	return err
}
