package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func parseConvID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的会话 ID")
		return 0, false
	}
	return id, true
}

// ---- 创建会话 ----

type createConvReq struct {
	Type    string  `json:"type"`     // direct | group
	UserID  *int64  `json:"user_id"`  // direct: 对方用户 ID
	Name    string  `json:"name"`     // group: 群名
	UserIDs []int64 `json:"user_ids"` // group: 初始成员 ID 列表
}

func handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	var req createConvReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	switch req.Type {
	case "direct":
		createDirect(w, r, me, &req)
	case "group":
		createGroup(w, r, me, &req)
	default:
		writeErr(w, http.StatusBadRequest, "type 只能是 direct 或 group")
	}
}

func createDirect(w http.ResponseWriter, r *http.Request, me *User, req *createConvReq) {
	if req.UserID == nil || *req.UserID == me.ID {
		writeErr(w, http.StatusBadRequest, "单聊需要指定对方用户 ID")
		return
	}
	peer, err := getUserByID(*req.UserID)
	if err != nil {
		if isNotFound(err) {
			writeErr(w, http.StatusNotFound, "对方用户不存在")
		} else {
			writeErr(w, http.StatusInternalServerError, "查询用户失败")
		}
		return
	}
	// 查找+创建在同一事务内，避免竞态
	var conv *Conversation
	var existed bool
	err = db.WithTx(func(t *tx) error {
		// 先在事务内查找
		var existingID int64
		err := t.QueryRow(`
			SELECT c.id FROM conversations c
			WHERE c.type='direct'
			  AND EXISTS(SELECT 1 FROM conversation_members cm WHERE cm.conversation_id=c.id AND cm.user_id=?)
			  AND EXISTS(SELECT 1 FROM conversation_members cm WHERE cm.conversation_id=c.id AND cm.user_id=?)
			LIMIT 1`, me.ID, peer.ID).Scan(&existingID)
		if err == nil {
			// 已存在
			existed = true
			conv = &Conversation{ID: existingID, Type: "direct", CreatedAt: nowMs()}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// 不存在，创建
		now := nowMs()
		res, err := t.Exec(`INSERT INTO conversations(type,name,owner_id,created_at) VALUES('direct','',NULL,?)`, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := t.Exec(`INSERT INTO conversation_members(conversation_id,user_id,joined_at) VALUES(?,?,?),(?,?,?)`,
			id, me.ID, now, id, peer.ID, now); err != nil {
			return err
		}
		conv = &Conversation{ID: id, Type: "direct", CreatedAt: now}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	conv.Members = []Member{
		{UserID: me.ID, Username: me.Username, DisplayName: me.DisplayName},
		{UserID: peer.ID, Username: peer.Username, DisplayName: peer.DisplayName},
	}
	if !existed {
		memberIDs := []int64{me.ID, peer.ID}
		hub.NotifyConversation(memberIDs, me.ID, conv)
		writeOK(w, http.StatusCreated, conv)
	} else {
		// 已存在，重新加载完整数据
		conv2, err := getConversationFor(conv.ID, me.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "加载会话失败")
			return
		}
		writeOK(w, http.StatusOK, conv2)
	}
}

func createGroup(w http.ResponseWriter, r *http.Request, me *User, req *createConvReq) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "群聊需要指定群名")
		return
	}
	if len([]rune(name)) > 50 {
		writeErr(w, http.StatusBadRequest, "群名过长（上限 50 字）")
		return
	}
	// 去重 + 排除自己
	seen := map[int64]bool{me.ID: true}
	userIDs := []int64{}
	for _, uid := range req.UserIDs {
		if uid > 0 && !seen[uid] {
			seen[uid] = true
			userIDs = append(userIDs, uid)
		}
	}
	if len(userIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "群聊至少需要邀请一名成员")
		return
	}
	// 校验所有用户存在
	for _, uid := range userIDs {
		if _, err := getUserByID(uid); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("用户 ID %d 不存在", uid))
			return
		}
	}

	var conv *Conversation
	err := db.WithTx(func(t *tx) error {
		res, err := t.Exec(`INSERT INTO conversations(type,name,owner_id,created_at) VALUES('group',?,?,?)`,
			name, me.ID, nowMs())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		now := nowMs()
		// 群主
		if _, err := t.Exec(`INSERT INTO conversation_members(conversation_id,user_id,joined_at) VALUES(?,?,?)`,
			id, me.ID, now); err != nil {
			return err
		}
		// 成员
		for _, uid := range userIDs {
			if _, err := t.Exec(`INSERT INTO conversation_members(conversation_id,user_id,joined_at) VALUES(?,?,?)`,
				id, uid, now); err != nil {
				return err
			}
		}
		conv = &Conversation{ID: id, Type: "group", Name: name, OwnerID: &me.ID, CreatedAt: now}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建群聊失败")
		return
	}
	conv.Members, _ = db.Members(conv.ID)
	allIDs := make([]int64, 0, len(conv.Members))
	for _, m := range conv.Members {
		allIDs = append(allIDs, m.UserID)
	}
	hub.NotifyConversation(allIDs, me.ID, conv)
	writeOK(w, http.StatusCreated, conv)
}

// ---- 会话列表 ----

func handleListConversations(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	convs, err := listConversationsFor(me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载会话列表失败")
		return
	}
	writeOK(w, http.StatusOK, convs)
}

func listConversationsFor(userID int64) ([]*Conversation, error) {
	rows, err := db.Query(`
		SELECT c.id, c.type, c.name, COALESCE(c.owner_id,0), c.created_at,
			COALESCE((SELECT MAX(m.id) FROM messages m WHERE m.conversation_id=c.id),0) AS last_msg_id,
			(SELECT COUNT(*) FROM messages m
			 WHERE m.conversation_id=c.id
			   AND m.id > COALESCE((SELECT last_read_id FROM read_marks rm
			                       WHERE rm.conversation_id=c.id AND rm.user_id=?),0)) AS unread
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id=c.id AND cm.user_id=?
		ORDER BY last_msg_id DESC, c.id DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []*Conversation
	msgIDs := []int64{}
	for rows.Next() {
		var c Conversation
		var ownerID int64
		var lastMsgID int64
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &ownerID, &c.CreatedAt, &lastMsgID, &c.Unread); err != nil {
			return nil, err
		}
		if ownerID > 0 {
			oid := ownerID
			c.OwnerID = &oid
		}
		convs = append(convs, &c)
		if lastMsgID > 0 {
			msgIDs = append(msgIDs, lastMsgID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(convs) == 0 {
		return []*Conversation{}, nil
	}

	// 批量加载最后一条消息
	msgs, err := db.MessagesByIDs(msgIDs)
	if err != nil {
		return nil, err
	}
	msgByID := map[int64]*Message{}
	for _, m := range msgs {
		msgByID[m.ID] = m
	}
	for _, c := range convs {
		// direct 会话：填充对方名称和成员信息（含头像）
		if c.Type == "direct" {
			ids, err := db.MemberIDs(c.ID)
			if err != nil {
				return nil, err
			}
			for _, uid := range ids {
				if uid != userID {
					if u, err := getUserByID(uid); err == nil {
						c.Name = u.DisplayName
						// 填充成员列表，客户端用它取对方头像
						c.Members = []Member{
							{UserID: u.ID, Username: u.Username, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL},
						}
					}
					break
				}
			}
		}
		for _, m := range msgByID {
			if m.ConversationID == c.ID {
				c.LastMessage = m
				break
			}
		}
	}
	return convs, nil
}

// ---- 会话详情 ----

func handleGetConversation(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	member, err := db.IsMember(id, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "你不是该会话成员")
		return
	}
	conv, err := getConversationFor(id, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载会话失败")
		return
	}
	writeOK(w, http.StatusOK, conv)
}

func getConversationFor(convID, userID int64) (*Conversation, error) {
	var c Conversation
	var ownerID int64
	err := db.QueryRow(`SELECT id,type,name,COALESCE(owner_id,0),created_at FROM conversations WHERE id=?`, convID).
		Scan(&c.ID, &c.Type, &c.Name, &ownerID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	if ownerID > 0 {
		oid := ownerID
		c.OwnerID = &oid
	}
	members, err := db.Members(convID)
	if err != nil {
		return nil, err
	}
	c.Members = members
	if c.Type == "direct" {
		for _, m := range members {
			if m.UserID != userID {
				c.Name = m.DisplayName
				break
			}
		}
	}
	unread, err := db.UnreadCount(convID, userID)
	if err != nil {
		return nil, err
	}
	c.Unread = unread
	lastID, err := db.LastMessageID(convID)
	if err == nil && lastID > 0 {
		if m, err := db.GetMessageByID(lastID); err == nil {
			c.LastMessage = m
		}
	}
	return &c, nil
}

// ---- 历史消息 ----

func handleListMessages(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	convID, ok := parseConvID(w, r)
	if !ok {
		return
	}
	member, err := db.IsMember(convID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "你不是该会话成员")
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	msgs, err := db.ListMessages(convID, before, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载消息失败")
		return
	}
	writeOK(w, http.StatusOK, msgs)
}

// ---- 已读回执 ----

func handleMarkRead(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	convID, ok := parseConvID(w, r)
	if !ok {
		return
	}
	member, err := db.IsMember(convID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "你不是该会话成员")
		return
	}
	lastID, err := db.LastMessageID(convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if err := db.MarkRead(convID, me.ID, lastID); err != nil {
		writeErr(w, http.StatusInternalServerError, "标记已读失败")
		return
	}
	writeOK(w, http.StatusOK, map[string]any{"last_read_id": lastID})
}

// ---- 群聊拉人 ----

type addMembersReq struct {
	UserIDs []int64 `json:"user_ids"`
}

func handleAddMembers(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	convID, ok := parseConvID(w, r)
	if !ok {
		return
	}
	member, err := db.IsMember(convID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "你不是该会话成员")
		return
	}
	// 只有 group 类型可以拉人
	var convType string
	err = db.QueryRow(`SELECT type FROM conversations WHERE id=?`, convID).Scan(&convType)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if convType != "group" {
		writeErr(w, http.StatusBadRequest, "单聊不能添加成员")
		return
	}

	var req addMembersReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if len(req.UserIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "需要指定 user_ids")
		return
	}

	// 去重 + 排除已有成员
	existingIDs, err := db.MemberIDs(convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询成员失败")
		return
	}
	existing := map[int64]bool{}
	for _, id := range existingIDs {
		existing[id] = true
	}
	seen := map[int64]bool{}
	var newIDs []int64
	for _, uid := range req.UserIDs {
		if uid > 0 && !existing[uid] && !seen[uid] {
			seen[uid] = true
			newIDs = append(newIDs, uid)
		}
	}
	if len(newIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "所有用户已是群成员")
		return
	}

	// 校验用户存在
	for _, uid := range newIDs {
		if _, err := getUserByID(uid); err != nil {
			writeErr(w, http.StatusBadRequest, "用户不存在")
			return
		}
	}

	now := nowMs()
	err = db.WithTx(func(t *tx) error {
		for _, uid := range newIDs {
			if _, err := t.Exec(`INSERT OR IGNORE INTO conversation_members(conversation_id,user_id,joined_at) VALUES(?,?,?)`,
				convID, uid, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "添加成员失败")
		return
	}

	conv, err := getConversationFor(convID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载会话失败")
		return
	}
	// 通知新成员刷新会话列表
	hub.NotifyConversation(newIDs, 0, conv)
	writeOK(w, http.StatusOK, conv)
}

// ---- 退群 ----

func handleLeaveConversation(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	convID, ok := parseConvID(w, r)
	if !ok {
		return
	}
	member, err := db.IsMember(convID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !member {
		writeErr(w, http.StatusForbidden, "你不是该会话成员")
		return
	}
	var convType string
	err = db.QueryRow(`SELECT type FROM conversations WHERE id=?`, convID).Scan(&convType)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if convType == "direct" {
		writeErr(w, http.StatusBadRequest, "单聊不支持退出")
		return
	}

	_, err = db.Exec(`DELETE FROM conversation_members WHERE conversation_id=? AND user_id=?`, convID, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "退群失败")
		return
	}
	writeOK(w, http.StatusOK, map[string]any{"left": true})
}
