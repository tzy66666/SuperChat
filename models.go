package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func nowMs() int64 { return time.Now().UnixMilli() }

func int64ToString(v int64) string { return strconv.FormatInt(v, 10) }

func stringToInt64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

// ---- 领域模型 ----

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarURL    string `json:"avatar_url"`
	CreatedAt    int64  `json:"created_at"`
	PasswordHash string `json:"-"`
}

type File struct {
	ID         int64  `json:"id"`
	StoredName string `json:"-"`
	OrigName   string `json:"name"`
	URL        string `json:"url"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	UploaderID int64  `json:"-"`
	CreatedAt  int64  `json:"created_at"`
}

type Message struct {
	ID             int64  `json:"id"`
	ConversationID int64  `json:"conversation_id"`
	SenderID       int64  `json:"sender_id"`
	SenderName     string `json:"sender_name"`   // 发送者昵称（客户端气泡旁头像用）
	SenderAvatar   string `json:"sender_avatar"` // 发送者头像 URL
	Type           string `json:"type"`
	Content        string `json:"content"`
	FileID         *int64 `json:"file_id,omitempty"`
	File           *File  `json:"file,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

type Member struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	JoinedAt    int64  `json:"joined_at"`
}

type Conversation struct {
	ID          int64    `json:"id"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	OwnerID     *int64   `json:"owner_id,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	Unread      int      `json:"unread"`
	LastMessage *Message `json:"last_message,omitempty"`
	Members     []Member `json:"members,omitempty"`
}

// ---- 事务封装 ----

type tx struct{ *sql.Tx }

func (d *DB) WithTx(fn func(*tx) error) error {
	t, err := d.Begin()
	if err != nil {
		return err
	}
	txw := &tx{t}
	if err := fn(txw); err != nil {
		_ = txw.Rollback()
		return err
	}
	return txw.Commit()
}

// ---- 错误辅助 ----

type conflictErr struct{ msg string }

func (e conflictErr) Error() string { return e.msg }

func errConflict(msg string) error { return conflictErr{msg} }

func isConflict(err error) bool {
	var ce conflictErr
	return errors.As(err, &ce)
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}

func isNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// ---- 用户 ----

const userCols = `id, username, password_hash, display_name, avatar_url, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.AvatarURL, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func getUserByID(id int64) (*User, error) {
	return scanUser(db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func getUserByIDTx(t *tx, id int64) (*User, error) {
	return scanUser(t.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func getUserByName(name string) (*User, error) {
	return scanUser(db.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, name))
}

func (d *DB) SearchUsers(q string, excludeID int64) ([]*User, error) {
	query := `SELECT ` + userCols + ` FROM users WHERE id<>?`
	args := []any{excludeID}
	if q != "" {
		query += ` AND (username LIKE ? OR display_name LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY id LIMIT 50`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if users == nil {
		users = []*User{} // 空结果统一返回 []，不返回 null（与其他列表接口一致）
	}
	return users, rows.Err()
}

// ---- 文件 ----

const fileCols = `id, stored_name, orig_name, mime, size, uploader_id, created_at`

func scanFile(row interface{ Scan(...any) error }) (*File, error) {
	var f File
	if err := row.Scan(&f.ID, &f.StoredName, &f.OrigName, &f.Mime, &f.Size, &f.UploaderID, &f.CreatedAt); err != nil {
		return nil, err
	}
	f.URL = fmt.Sprintf("/files/%d/%s", f.ID, f.StoredName)
	return &f, nil
}

func (d *DB) GetFileByID(id int64) (*File, error) {
	return scanFile(d.QueryRow(`SELECT `+fileCols+` FROM files WHERE id=?`, id))
}

func (d *DB) GetFileByStored(id int64, storedName string) (*File, error) {
	return scanFile(d.QueryRow(`SELECT `+fileCols+` FROM files WHERE id=? AND stored_name=?`, id, storedName))
}

// ---- 消息 ----

// 消息查询：LEFT JOIN users 带出发送者昵称和头像（用户异常删除时降级为空，不影响消息返回）
const msgCols = `m.id, m.conversation_id, m.sender_id, m.type, m.content, COALESCE(m.file_id,0), m.created_at, COALESCE(u.display_name,''), COALESCE(u.avatar_url,'')`
const msgJoin = ` FROM messages m LEFT JOIN users u ON u.id = m.sender_id `

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var fileID int64
	if err := row.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Type, &m.Content, &fileID, &m.CreatedAt, &m.SenderName, &m.SenderAvatar); err != nil {
		return nil, err
	}
	if fileID != 0 {
		m.FileID = &fileID
	}
	return &m, nil
}

func (d *DB) GetMessageByID(id int64) (*Message, error) {
	m, err := scanMessage(d.QueryRow(`SELECT `+msgCols+msgJoin+`WHERE m.id=?`, id))
	if err != nil {
		return nil, err
	}
	if err := d.fillMessageFile(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (d *DB) ListMessages(convID, beforeID int64, limit int) ([]*Message, error) {
	query := `SELECT ` + msgCols + msgJoin + `WHERE m.conversation_id=?`
	args := []any{convID}
	if beforeID > 0 {
		query += ` AND m.id<?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY m.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 逆序为正序
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	for _, m := range msgs {
		if err := d.fillMessageFile(m); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

func (d *DB) fillMessageFile(m *Message) error {
	if m.FileID == nil {
		return nil
	}
	f, err := d.GetFileByID(*m.FileID)
	if err != nil {
		if isNotFound(err) {
			m.FileID = nil // 文件已删除，降级为无附件
			return nil
		}
		return err
	}
	m.File = f
	return nil
}

func (d *DB) MessageByIDTx(t *tx, id int64) (*Message, error) {
	m, err := scanMessage(t.QueryRow(`SELECT `+msgCols+msgJoin+`WHERE m.id=?`, id))
	if err != nil {
		return nil, err
	}
	if m.FileID != nil {
		f, err := scanFile(t.QueryRow(`SELECT `+fileCols+` FROM files WHERE id=?`, *m.FileID))
		if err != nil {
			return nil, err
		}
		m.File = f
	}
	return m, nil
}

func (d *DB) MessagesByIDs(ids []int64) ([]*Message, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.Query(`SELECT `+msgCols+msgJoin+`WHERE m.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if err := d.fillMessageFile(m); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

// ---- 通讯录 ----

// Contacts 返回与指定用户共处过任意会话的其他用户（去重），供群聊拉人选择
func (d *DB) Contacts(userID int64) ([]*User, error) {
	rows, err := d.Query(`
		SELECT DISTINCT u.id, u.username, u.password_hash, u.display_name, u.avatar_url, u.created_at
		FROM conversation_members mine
		JOIN conversation_members other ON other.conversation_id = mine.conversation_id
		JOIN users u ON u.id = other.user_id
		WHERE mine.user_id = ? AND u.id <> ?
		ORDER BY u.id`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if users == nil {
		users = []*User{} // 空结果统一返回 []，不返回 null（与其他列表接口一致）
	}
	return users, rows.Err()
}

// ---- 会话 ----

func (d *DB) IsMember(convID, userID int64) (bool, error) {
	var n int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM conversation_members WHERE conversation_id=? AND user_id=?`,
		convID, userID).Scan(&n)
	return n > 0, err
}

func (d *DB) MemberIDs(convID int64) ([]int64, error) {
	rows, err := d.Query(
		`SELECT user_id FROM conversation_members WHERE conversation_id=? ORDER BY joined_at`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (d *DB) Members(convID int64) ([]Member, error) {
	rows, err := d.Query(`
		SELECT cm.user_id, u.username, u.display_name, u.avatar_url, cm.joined_at
		FROM conversation_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.conversation_id=? ORDER BY cm.joined_at`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// FindDirectConversation 查找两个用户间已有的单聊会话
func (d *DB) FindDirectConversation(a, b int64) (int64, error) {
	var id int64
	err := d.QueryRow(`
		SELECT c.id FROM conversations c
		WHERE c.type='direct'
		  AND EXISTS(SELECT 1 FROM conversation_members cm WHERE cm.conversation_id=c.id AND cm.user_id=?)
		  AND EXISTS(SELECT 1 FROM conversation_members cm WHERE cm.conversation_id=c.id AND cm.user_id=?)
		LIMIT 1`, a, b).Scan(&id)
	return id, err
}

func (d *DB) MarkRead(convID, userID, lastID int64) error {
	_, err := d.Exec(`
		INSERT INTO read_marks(conversation_id,user_id,last_read_id) VALUES(?,?,?)
		ON CONFLICT(conversation_id,user_id) DO UPDATE SET last_read_id=excluded.last_read_id`,
		convID, userID, lastID)
	return err
}

func (d *DB) LastMessageID(convID int64) (int64, error) {
	var id int64
	err := d.QueryRow(`SELECT COALESCE(MAX(id),0) FROM messages WHERE conversation_id=?`, convID).Scan(&id)
	return id, err
}

func (d *DB) UnreadCount(convID, userID int64) (int, error) {
	var n int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM messages m
		WHERE m.conversation_id=?
		  AND m.id > COALESCE((SELECT last_read_id FROM read_marks
		                      WHERE conversation_id=? AND user_id=?),0)`,
		convID, convID, userID).Scan(&n)
	return n, err
}
