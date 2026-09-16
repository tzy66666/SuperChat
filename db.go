package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB 封装 SQLite 连接。设最大 1 连接，串行写入，对 1 核 VPS 最稳。
type DB struct {
	*sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT    NOT NULL UNIQUE,
	password_hash TEXT    NOT NULL,
	display_name  TEXT    NOT NULL DEFAULT '',
	created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS conversations (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	type      TEXT NOT NULL CHECK(type IN ('direct','group')),
	name      TEXT NOT NULL DEFAULT '',
	owner_id  INTEGER,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS conversation_members (
	conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	joined_at       INTEGER NOT NULL,
	PRIMARY KEY (conversation_id, user_id)
);

CREATE TABLE IF NOT EXISTS files (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	stored_name TEXT    NOT NULL UNIQUE,
	orig_name   TEXT    NOT NULL,
	mime        TEXT    NOT NULL DEFAULT 'application/octet-stream',
	size        INTEGER NOT NULL,
	uploader_id INTEGER NOT NULL REFERENCES users(id),
	created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	sender_id       INTEGER NOT NULL REFERENCES users(id),
	type            TEXT    NOT NULL DEFAULT 'text' CHECK(type IN ('text','image','file')),
	content         TEXT    NOT NULL DEFAULT '',
	file_id         INTEGER REFERENCES files(id) ON DELETE SET NULL,
	created_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS read_marks (
	conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	last_read_id    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id, id);
CREATE INDEX IF NOT EXISTS idx_members_user ON conversation_members(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender_id);
`

func openDB(path string) (*DB, error) {
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	d.SetMaxOpenConns(1)
	if err := d.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	if _, err := d.Exec(schema); err != nil {
		return nil, fmt.Errorf("初始化表结构: %w", err)
	}
	// 迁移：加 avatar_url 字段（如果不存在）
	if _, err := d.Exec(`ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''`); err != nil {
		// 字段已存在会报错，忽略即可
		if !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("迁移 avatar_url: %w", err)
		}
	}
	return &DB{d}, nil
}
