package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ---- JWT ----

const tokenTTL = 30 * 24 * time.Hour

func loadJWTKey(path string) error {
	if b, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) >= 16 {
			jwtKey = []byte(s)
			return nil
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	jwtKey = []byte(hex.EncodeToString(key))
	if err := os.WriteFile(path, jwtKey, 0o600); err != nil {
		return err
	}
	log.Printf("已生成新的 JWT 密钥: %s", path)
	return nil
}

func issueToken(u *User) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   int64ToString(u.ID),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtKey)
}

func parseToken(tokenStr string) (*User, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return jwtKey, nil
	})
	if err != nil || !tok.Valid {
		return nil, err
	}
	claims := tok.Claims.(*jwt.RegisteredClaims)
	id, err := stringToInt64(claims.Subject)
	if err != nil {
		return nil, err
	}
	return getUserByID(id)
}

func userFromRequest(r *http.Request) (*User, error) {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return parseToken(strings.TrimPrefix(auth, "Bearer "))
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return parseToken(t)
	}
	return nil, jwt.ErrTokenMalformed
}

// ---- 注册 / 登录 ----

type registerReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type authResp struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if len(req.Username) < 2 || len(req.Username) > 32 {
		writeErr(w, http.StatusBadRequest, "用户名长度需在 2-32 个字符之间")
		return
	}
	if !validName(req.Username) {
		writeErr(w, http.StatusBadRequest, "用户名只能包含字母、数字、下划线、连字符")
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 72 {
		writeErr(w, http.StatusBadRequest, "密码长度需在 6-72 个字符之间")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	var u *User
	err = db.WithTx(func(tx *tx) error {
		res, err := tx.Exec(`INSERT INTO users(username,password_hash,display_name,created_at) VALUES(?,?,?,?)`,
			req.Username, string(hash), req.DisplayName, nowMs())
		if err != nil {
			if isUniqueViolation(err) {
				return errConflict("用户名已被占用")
			}
			return err
		}
		id, _ := res.LastInsertId()
		u, err = getUserByIDTx(tx, id)
		return err
	})
	if err != nil {
		if isConflict(err) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "注册失败")
		return
	}
	token, err := issueToken(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "签发 token 失败")
		return
	}
	writeOK(w, http.StatusCreated, authResp{Token: token, User: u})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	u, err := getUserByName(req.Username)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := issueToken(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "签发 token 失败")
		return
	}
	writeOK(w, http.StatusOK, authResp{Token: token, User: u})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	writeOK(w, http.StatusOK, currentUser(r))
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	users, err := db.SearchUsers(q, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询用户失败")
		return
	}
	writeOK(w, http.StatusOK, users)
}

func validName(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ---- 头像 ----

// handleUploadAvatar 用户上传自己的头像（multipart, 字段名 file）
func handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20) // 头像上限 8MB
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "上传失败：文件过大或格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadRequest, "头像必须是图片")
		return
	}

	// 复用文件上传逻辑
	storedName := randomHex(16) + ".jpg"
	sub := filepath.Join(uploadsDir(), timeNowDir())
	if err := os.MkdirAll(sub, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建目录失败")
		return
	}
	dst, err := os.Create(filepath.Join(sub, storedName))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "写入文件失败")
		return
	}
	n, copyErr := io.Copy(dst, file)
	dst.Close()
	if copyErr != nil || n == 0 {
		os.Remove(filepath.Join(sub, storedName))
		writeErr(w, http.StatusInternalServerError, "保存文件失败")
		return
	}

	rel := filepath.ToSlash(filepath.Join(timeNowDir(), storedName))
	avatarURL := "/api/avatars/" + rel

	// 更新用户头像 URL
	_, err = db.Exec(`UPDATE users SET avatar_url=? WHERE id=?`, avatarURL, me.ID)
	if err != nil {
		os.Remove(filepath.Join(sub, storedName))
		writeErr(w, http.StatusInternalServerError, "更新头像失败")
		return
	}

	// 返回最新用户信息
	u, err := getUserByID(me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取用户信息失败")
		return
	}
	writeOK(w, http.StatusOK, u)
}

// handleGetAvatar 提供头像图片（无需鉴权，URL 含随机路径不可猜测）
func handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest") // "2026/09/xxxx.jpg" 格式
	if rest == "" {
		writeErr(w, http.StatusNotFound, "头像不存在")
		return
	}
	full := filepath.Join(uploadsDir(), filepath.FromSlash(rest))
	if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(uploadsDir())+string(os.PathSeparator)) {
		writeErr(w, http.StatusNotFound, "头像不存在")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, full)
}
