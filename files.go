package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func uploadsDir() string {
	return filepath.Join(cfg.DataDir, "uploads")
}

// handleUploadFile 接收 multipart/form-data，字段名 file
func handleUploadFile(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxUpload+1<<20)
	if err := r.ParseMultipartForm(cfg.MaxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "上传失败：文件过大或表单格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()
	if header.Size > cfg.MaxUpload {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("文件超过上限 %d MB", cfg.MaxUpload>>20))
		return
	}

	// 不可猜测的存储名：随机 32 hex + 原扩展名
	storedName := randomHex(16)
	if ext := filepath.Ext(header.Filename); len(ext) <= 12 && ext != "" {
		storedName += strings.ToLower(ext)
	}
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	ct = sanitizeMime(ct)

	sub := filepath.Join(uploadsDir(), timeNowDir())
	if err := os.MkdirAll(sub, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建存储目录失败")
		return
	}
	dst, err := os.Create(filepath.Join(sub, storedName))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "写入文件失败")
		return
	}
	n, copyErr := io.Copy(dst, file)
	dst.Close()
	if copyErr != nil {
		os.Remove(filepath.Join(sub, storedName))
		writeErr(w, http.StatusInternalServerError, "保存文件失败")
		return
	}

	// 数据库记录（存相对 uploads 的路径）
	rel := filepath.ToSlash(filepath.Join(timeNowDir(), storedName))
	res, err := db.Exec(`INSERT INTO files(stored_name,orig_name,mime,size,uploader_id,created_at) VALUES(?,?,?,?,?,?)`,
		rel, header.Filename, ct, n, me.ID, nowMs())
	if err != nil {
		os.Remove(filepath.Join(sub, storedName))
		writeErr(w, http.StatusInternalServerError, "写入文件记录失败")
		return
	}
	id, _ := res.LastInsertId()
	f, err := db.GetFileByID(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取文件记录失败")
		return
	}
	writeOK(w, http.StatusCreated, f)
}

// handleGetFile 提供文件下载/预览。URL: /files/{id}/{rest...}
func handleGetFile(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	rest := r.PathValue("rest")
	id, err := stringToInt64(idStr)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的文件 ID")
		return
	}
	f, err := db.GetFileByStored(id, rest)
	if err != nil {
		writeErr(w, http.StatusNotFound, "文件不存在")
		return
	}
	full := filepath.Join(uploadsDir(), filepath.FromSlash(f.StoredName))
	// 路径穿越防护
	if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(uploadsDir())+string(os.PathSeparator)) {
		writeErr(w, http.StatusNotFound, "文件不存在")
		return
	}
	w.Header().Set("Content-Type", f.Mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename*=UTF-8''%s", urlEncodeName(f.OrigName)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, full)
}

// sanitizeMime 只放行安全类型
func sanitizeMime(ct string) string {
	base := strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch {
	case strings.HasPrefix(base, "image/"),
		base == "application/pdf",
		base == "application/zip",
		base == "application/gzip",
		base == "text/plain",
		strings.HasPrefix(base, "audio/"),
		strings.HasPrefix(base, "video/"),
		base == "application/octet-stream":
		return base
	default:
		return "application/octet-stream"
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func timeNowDir() string {
	return time.Now().Format("2006/01")
}

func urlEncodeName(s string) string {
	r := strings.NewReplacer("%", "%25", `"`, "%22", "\n", "%0A", "\r", "%0D")
	return r.Replace(s)
}
