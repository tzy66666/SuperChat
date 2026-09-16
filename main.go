// messenger - 通讯软件服务端
// 单个 Go 二进制 + 一个 SQLite 文件 + 一个 uploads 目录
// 外部由 Caddy 反代提供 HTTPS
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var (
	cfg    Config
	db     *DB
	hub    = newHub()
	jwtKey []byte
)

type Config struct {
	Listen    string // 监听地址
	DataDir   string // 数据目录（gochat.db + uploads/）
	MaxUpload int64  // 单文件上传上限（字节）
}

func main() {
	flag.StringVar(&cfg.Listen, "listen", ":8080", "监听地址，例如 :8080 或 127.0.0.1:8080")
	flag.StringVar(&cfg.DataDir, "data", "./data", "数据目录（存放 messenger.db 和 uploads/）")
	flag.Int64Var(&cfg.MaxUpload, "max-upload", 32<<20, "单文件上传上限（字节），默认 32MB")
	flag.Parse()

	cfg.MaxUpload = min(cfg.MaxUpload, 64<<20)

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	if err := loadJWTKey(filepath.Join(cfg.DataDir, "jwt_secret")); err != nil {
		log.Fatalf("初始化 JWT 密钥失败: %v", err)
	}
	d, err := openDB(filepath.Join(cfg.DataDir, "messenger.db"))
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	db = d
	defer db.Close()

	mux := http.NewServeMux()
	routes(mux)

	handler := logRequests(mux)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("messenger 已启动，监听 %s，数据目录 %s", cfg.Listen, cfg.DataDir)
	log.Printf("上传上限 %d MB，uploads 目录: %s", cfg.MaxUpload>>20, filepath.Join(cfg.DataDir, "uploads"))
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("服务已关闭")
}

func routes(m *http.ServeMux) {
	// 认证
	m.HandleFunc("POST /api/auth/register", handleRegister)
	m.HandleFunc("POST /api/auth/login", handleLogin)
	// 用户
	m.HandleFunc("GET /api/me", withAuth(handleMe))
	m.HandleFunc("GET /api/users", withAuth(handleListUsers))
	m.HandleFunc("GET /api/contacts", withAuth(handleListContacts))
	m.HandleFunc("POST /api/avatar", withAuth(handleUploadAvatar))
	m.HandleFunc("GET /api/avatars/{rest...}", handleGetAvatar)
	// 会话
	m.HandleFunc("GET /api/conversations", withAuth(handleListConversations))
	m.HandleFunc("POST /api/conversations", withAuth(handleCreateConversation))
	m.HandleFunc("GET /api/conversations/{id}", withAuth(handleGetConversation))
	m.HandleFunc("GET /api/conversations/{id}/messages", withAuth(handleListMessages))
	m.HandleFunc("POST /api/conversations/{id}/read", withAuth(handleMarkRead))
	m.HandleFunc("POST /api/conversations/{id}/members", withAuth(handleAddMembers))
	m.HandleFunc("DELETE /api/conversations/{id}/members/me", withAuth(handleLeaveConversation))
	// 消息
	m.HandleFunc("POST /api/messages", withAuth(handleSendMessage))
	// 文件
	m.HandleFunc("POST /api/files", withAuth(handleUploadFile))
	m.HandleFunc("GET /files/{id}/{rest...}", handleGetFile)
	// WebSocket
	m.HandleFunc("GET /api/ws", handleWS)
}

// ---- HTTP 工具 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type envelope struct {
	OK   bool `json:"ok"`
	Data any  `json:"data,omitempty"`
}

func writeOK(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, envelope{OK: true, Data: data})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

type ctxUserKey struct{}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := userFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "未登录或登录已过期")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey{}, u)
		next(w, r.WithContext(ctx))
	}
}

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(ctxUserKey{}).(*User)
	return u
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path == "/api/ws" {
			return
		}
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
