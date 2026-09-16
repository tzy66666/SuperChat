package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type sendMsgReq struct {
	ConversationID int64  `json:"conversation_id"`
	Type            string `json:"type"`    // text | image | file（可省略，按 file mime 自动判断）
	Content         string `json:"content"` // 文字消息内容
	FileID          *int64 `json:"file_id"`  // 媒体消息的文件 ID
}

func handleSendMessage(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	var req sendMsgReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if req.ConversationID <= 0 {
		writeErr(w, http.StatusBadRequest, "缺少 conversation_id")
		return
	}
	member, err := db.IsMember(req.ConversationID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "你不是该会话成员")
		return
	}

	req.Content = strings.TrimSpace(req.Content)
	msgType := strings.ToLower(req.Type)
	if msgType == "" {
		msgType = "text"
	}

	// 如果有 file_id，先校验文件
	var file *File
	if req.FileID != nil {
		file, err = db.GetFileByID(*req.FileID)
		if err != nil || file == nil {
			writeErr(w, http.StatusBadRequest, "文件不存在")
			return
		}
		// 校验文件归属：只能引用自己上传的文件
		if file.UploaderID != me.ID {
			writeErr(w, http.StatusForbidden, "无权使用该文件")
			return
		}
		// 自动推断消息类型
		if msgType == "text" {
			if strings.HasPrefix(file.Mime, "image/") {
				msgType = "image"
			} else {
				msgType = "file"
			}
		}
	}

	switch msgType {
	case "text":
		if req.Content == "" {
			writeErr(w, http.StatusBadRequest, "文字消息内容不能为空")
			return
		}
		if len([]rune(req.Content)) > 4000 {
			writeErr(w, http.StatusBadRequest, "消息内容过长（上限 4000 字）")
			return
		}
	case "image", "file":
		if req.FileID == nil {
			writeErr(w, http.StatusBadRequest, "媒体消息必须携带 file_id")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "消息类型只能是 text/image/file")
		return
	}

	var m *Message
	err = db.WithTx(func(t *tx) error {
		var fileID any
		if req.FileID != nil {
			fileID = *req.FileID
		}
		res, err := t.Exec(`INSERT INTO messages(conversation_id,sender_id,type,content,file_id,created_at) VALUES(?,?,?,?,?,?)`,
			req.ConversationID, me.ID, msgType, req.Content, fileID, nowMs())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		m, err = db.MessageByIDTx(t, id)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "发送消息失败")
		return
	}

	// WebSocket 推送给会话所有成员（含发送者，便于多端同步）
	memberIDs, err := db.MemberIDs(req.ConversationID)
	if err == nil {
		hub.NotifyTo(memberIDs, map[string]any{"type": "message", "data": m})
	}
	writeOK(w, http.StatusCreated, m)
}
