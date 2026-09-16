// SuperChat Admin - 服务端管理后台
// 只监听本机地址，通过 SSH 隧道在本地浏览器访问：
//   ssh -p 60791 -L 8090:127.0.0.1:8090 root@104.233.158.113
//   然后打开 http://127.0.0.1:8090
package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed static/index.html
var staticFS embed.FS

type server struct {
	db       *sql.DB
	uploads  string
	upstream string
	password string
	started  time.Time

	mu     sync.Mutex
	tokens map[string]time.Time // token -> 过期时间
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8090", "监听地址（只绑本机，用 SSH 隧道访问）")
	dbPath := flag.String("db", "./data/messenger.db", "messenger 数据库路径")
	uploads := flag.String("uploads", "./data/uploads", "uploads 目录")
	upstream := flag.String("upstream", "", "messenger 对外地址（用于显示头像），如 https://x.com:8443")
	password := flag.String("password", "", "登录密码（必填）")
	flag.Parse()

	if *password == "" {
		log.Fatal("必须用 -password 指定登录密码")
	}

	// 与 messenger 相同的 DSN：WAL + busy_timeout，多进程并发安全
	dsn := "file:" + filepath.ToSlash(*dbPath) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	d.SetMaxOpenConns(1)
	if err := d.Ping(); err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	s := &server{
		db:       d,
		uploads:  *uploads,
		upstream: strings.TrimSuffix(*upstream, "/"),
		password: *password,
		started:  time.Now(),
		tokens:   make(map[string]time.Time),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/overview", s.auth(s.handleOverview))
	mux.HandleFunc("GET /api/users", s.auth(s.handleUsers))
	mux.HandleFunc("DELETE /api/users/{id}", s.auth(s.handleDeleteUser))
	mux.HandleFunc("GET /api/conversations", s.auth(s.handleConversations))
	mux.HandleFunc("GET /api/conversations/{id}/messages", s.auth(s.handleConvMessages))
	mux.HandleFunc("DELETE /api/conversations/{id}", s.auth(s.handleDeleteConversation))
	mux.HandleFunc("GET /api/files", s.auth(s.handleFiles))
	mux.HandleFunc("DELETE /api/files/{id}", s.auth(s.handleDeleteFile))

	log.Printf("SuperChat 管理后台已启动，监听 %s", *listen)
	log.Printf("数据库: %s", *dbPath)
	srv := &http.Server{Addr: *listen, Handler: logReq(mux)}
	log.Fatal(srv.ListenAndServe())
}

func logReq(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// ---- HTTP 工具 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

// ---- 认证 ----

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if !subtleConstEq(req.Password, s.password) {
		// 防爆破：密码错误时延迟响应
		time.Sleep(800 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "密码错误")
		return
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	s.mu.Lock()
	// 顺手清理过期 token
	now := time.Now()
	for t, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, t)
		}
	}
	s.tokens[token] = now.Add(24 * time.Hour)
	s.mu.Unlock()
	writeOK(w, map[string]string{"token": token})
}

func subtleConstEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		exp, ok := s.tokens[token]
		if ok && time.Now().After(exp) {
			delete(s.tokens, token)
			ok = false
		}
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusUnauthorized, "未登录或登录已过期")
			return
		}
		next(w, r)
	}
}

// ---- 页面 ----

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "页面加载失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// ---- 概览 ----

func (s *server) handleOverview(w http.ResponseWriter, r *http.Request) {
	var stats struct {
		Users       int64 `json:"users"`
		Convs       int64 `json:"conversations"`
		Messages    int64 `json:"messages"`
		Files       int64 `json:"files"`
		FilesSize   int64 `json:"files_size"`
		UploadsSize int64 `json:"uploads_size"`
	}
	_ = s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM conversations),
		(SELECT COUNT(*) FROM messages),
		(SELECT COUNT(*) FROM files),
		(SELECT COALESCE(SUM(size),0) FROM files)`).Scan(
		&stats.Users, &stats.Convs, &stats.Messages, &stats.Files, &stats.FilesSize)

	stats.UploadsSize = dirSize(s.uploads)

	memTotal, memUsed, diskTotal, diskUsed := sysInfo()
	overview := map[string]any{
		"stats":            stats,
		"mem_total":        memTotal,
		"mem_used":         memUsed,
		"disk_total":       diskTotal,
		"disk_used":        diskUsed,
		"messenger_active": serviceActive("messenger"),
		"admin_active":     serviceActive("schat-admin"),
		"admin_uptime":     int64(time.Since(s.started).Seconds()),
		"upstream":         s.upstream,
	}
	writeOK(w, overview)
}

// ---- 用户 ----

func (s *server) handleUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, u.created_at,
		       (SELECT COUNT(*) FROM conversation_members cm WHERE cm.user_id = u.id),
		       (SELECT COUNT(*) FROM messages m WHERE m.sender_id = u.id)
		FROM users u ORDER BY u.id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询用户失败")
		return
	}
	defer rows.Close()
	type user struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Name      string `json:"display_name"`
		AvatarURL string `json:"avatar_url"`
		CreatedAt int64  `json:"created_at"`
		Convs     int64  `json:"conv_count"`
		Msgs      int64  `json:"msg_count"`
	}
	var users []user
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.Convs, &u.Msgs); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取用户失败")
			return
		}
		users = append(users, u)
	}
	if users == nil {
		users = []user{}
	}
	writeOK(w, users)
}

// 删除用户：级联清理其消息、参与的单聊会话（整体删除）、群聊成员关系、
// 上传的文件（记录+磁盘）、头像文件
func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的用户 ID")
		return
	}

	var diskFiles []string
	var avatarRel string

	err := withTx(s.db, func(tx *sql.Tx) error {
		// 收集待删磁盘文件：该用户上传的文件
		rows, err := tx.Query(`SELECT stored_name FROM files WHERE uploader_id=?`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			diskFiles = append(diskFiles, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// 头像文件（avatar_url 形如 /api/avatars/2026/09/x.jpg）
		var avatarURL string
		if err := tx.QueryRow(`SELECT avatar_url FROM users WHERE id=?`, id).Scan(&avatarURL); err != nil {
			if err == sql.ErrNoRows {
				return errUserNotFound
			}
			return err
		}
		avatarRel = strings.TrimPrefix(avatarURL, "/api/avatars/")

		// 1. 该用户参与的单聊会话：整体删除
		//    注意：必须在删除成员关系之前执行（子查询依赖成员表定位单聊），
		//    外键 ON DELETE CASCADE 会连带清理该会话的 messages/read_marks/members
		stmts := []string{
			`DELETE FROM conversations WHERE type='direct' AND id IN (SELECT c.id FROM conversations c
				JOIN conversation_members m ON m.conversation_id = c.id
				WHERE c.type='direct' AND m.user_id=?)`,
			// 兜底：清掉可能残留的单聊附属数据（外键级联失效时保证一致性）
			`DELETE FROM messages WHERE conversation_id NOT IN (SELECT id FROM conversations)`,
			`DELETE FROM read_marks WHERE conversation_id NOT IN (SELECT id FROM conversations)`,
			`DELETE FROM conversation_members WHERE conversation_id NOT IN (SELECT id FROM conversations)`,
			// 2. 该用户发的其他消息（群聊里的）
			`DELETE FROM messages WHERE sender_id=?`,
			// 3. 群聊成员关系 / 已读标记
			`DELETE FROM conversation_members WHERE user_id=?`,
			`DELETE FROM read_marks WHERE user_id=?`,
			// 4. 上传文件记录 / 用户
			`DELETE FROM files WHERE uploader_id=?`,
			`DELETE FROM users WHERE id=?`,
		}
		for _, q := range stmts {
			if _, err := tx.Exec(q, id, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err == errUserNotFound {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除用户失败: "+err.Error())
		return
	}

	// 事务成功后再删磁盘文件（失败不影响数据一致性）
	for _, n := range diskFiles {
		_ = os.Remove(filepath.Join(s.uploads, filepath.FromSlash(n)))
	}
	if avatarRel != "" {
		_ = os.Remove(filepath.Join(s.uploads, filepath.FromSlash(avatarRel)))
	}
	writeOK(w, map[string]any{"deleted": id})
}

// ---- 会话 ----

func (s *server) handleConversations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT c.id, c.type, c.name, c.created_at,
		       (SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id = c.id),
		       (SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id),
		       (SELECT MAX(created_at) FROM messages m WHERE m.conversation_id = c.id),
		       (SELECT GROUP_CONCAT(u.display_name, '、') FROM conversation_members cm2
		        JOIN users u ON u.id = cm2.user_id WHERE cm2.conversation_id = c.id)
		FROM conversations c ORDER BY c.id DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询会话失败")
		return
	}
	defer rows.Close()
	type conv struct {
		ID        int64  `json:"id"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		CreatedAt int64  `json:"created_at"`
		Members   int64  `json:"member_count"`
		Msgs      int64  `json:"msg_count"`
		LastAt    int64  `json:"last_active_at"`
		MembersTx string `json:"members_text"`
	}
	var convs []conv
	for rows.Next() {
		var c conv
		var lastAt sql.NullInt64
		var membersTx sql.NullString
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &c.CreatedAt, &c.Members, &c.Msgs, &lastAt, &membersTx); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取会话失败")
			return
		}
		c.LastAt = lastAt.Int64
		c.MembersTx = membersTx.String
		convs = append(convs, c)
	}
	if convs == nil {
		convs = []conv{}
	}
	writeOK(w, convs)
}

// 会话消息浏览（只读，最新的在前，最多 500 条）
func (s *server) handleConvMessages(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的会话 ID")
		return
	}
	rows, err := s.db.Query(`
		SELECT m.id, m.sender_id, COALESCE(u.display_name, '已删除用户'),
		       m.type, m.content, COALESCE(f.orig_name, ''), COALESCE(f.size, 0), m.created_at
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		LEFT JOIN files f ON f.id = m.file_id
		WHERE m.conversation_id = ?
		ORDER BY m.id DESC LIMIT 500`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询消息失败")
		return
	}
	defer rows.Close()
	type msg struct {
		ID        int64  `json:"id"`
		SenderID  int64  `json:"sender_id"`
		Sender    string `json:"sender_name"`
		Type      string `json:"type"`
		Content   string `json:"content"`
		FileName  string `json:"file_name"`
		FileSize  int64  `json:"file_size"`
		CreatedAt int64  `json:"created_at"`
	}
	var msgs []msg
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Sender, &m.Type, &m.Content, &m.FileName, &m.FileSize, &m.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取消息失败")
			return
		}
		msgs = append(msgs, m)
	}
	if msgs == nil {
		msgs = []msg{}
	}
	writeOK(w, msgs)
}

func (s *server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的会话 ID")
		return
	}
	err := withTx(s.db, func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM conversations WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errConvNotFound
		}
		// messages/read_marks/members 由外键 ON DELETE CASCADE 级联
		return nil
	})
	if err == errConvNotFound {
		writeErr(w, http.StatusNotFound, "会话不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除会话失败: "+err.Error())
		return
	}
	writeOK(w, map[string]any{"deleted": id})
}

// ---- 文件 ----

func (s *server) handleFiles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT f.id, f.stored_name, f.orig_name, f.mime, f.size,
		       COALESCE(u.display_name, '已删除用户'), f.created_at
		FROM files f LEFT JOIN users u ON u.id = f.uploader_id
		ORDER BY f.id DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询文件失败")
		return
	}
	defer rows.Close()
	type file struct {
		ID        int64  `json:"id"`
		Stored    string `json:"stored_name"`
		Name      string `json:"orig_name"`
		Mime      string `json:"mime"`
		Size      int64  `json:"size"`
		Uploader  string `json:"uploader_name"`
		CreatedAt int64  `json:"created_at"`
	}
	var files []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.ID, &f.Stored, &f.Name, &f.Mime, &f.Size, &f.Uploader, &f.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取文件失败")
			return
		}
		files = append(files, f)
	}
	if files == nil {
		files = []file{}
	}
	writeOK(w, files)
}

func (s *server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的文件 ID")
		return
	}
	var stored string
	err := withTx(s.db, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT stored_name FROM files WHERE id=?`, id).Scan(&stored); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM files WHERE id=?`, id)
		return err
	})
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "文件不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除文件失败: "+err.Error())
		return
	}
	if stored != "" {
		_ = os.Remove(filepath.Join(s.uploads, filepath.FromSlash(stored)))
	}
	writeOK(w, map[string]any{"deleted": id})
}

// ---- 事务与辅助 ----

var (
	errUserNotFound = fmt.Errorf("user not found")
	errConvNotFound = fmt.Errorf("conversation not found")
)

func withTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// dirSize 递归统计目录大小（字节）
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 忽略无法访问的项
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// sysInfo 读取内存与磁盘（Linux；失败返回 0，前端显示为 —）
func sysInfo() (memTotal, memUsed, diskTotal, diskUsed int64) {
	if out, err := exec.Command("free", "-b").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] == "Mem:" {
				memTotal, _ = strconv.ParseInt(fields[1], 10, 64)
				memUsed, _ = strconv.ParseInt(fields[2], 10, 64)
				break
			}
		}
	}
	if out, err := exec.Command("df", "-B1", "--output=size,used", "/").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 2 {
				diskTotal, _ = strconv.ParseInt(fields[0], 10, 64)
				diskUsed, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}
	return
}

// serviceActive 查询 systemd 服务状态（失败返回 unknown）
func serviceActive(name string) string {
	out, err := exec.Command("systemctl", "is-active", name).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
