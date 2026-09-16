package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Hub 维护 user_id -> 在线 WebSocket 连接集合
type Hub struct {
	mu    sync.Mutex
	users map[int64]map[*websocket.Conn]struct{}
}

func newHub() *Hub {
	return &Hub{users: map[int64]map[*websocket.Conn]struct{}{}}
}

func (h *Hub) register(userID int64, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.users[userID] == nil {
		h.users[userID] = map[*websocket.Conn]struct{}{}
	}
	h.users[userID][c] = struct{}{}
}

func (h *Hub) unregister(userID int64, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns, ok := h.users[userID]; ok {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.users, userID)
		}
	}
}

// NotifyTo 给指定用户集合推送事件（锁内只做快照，锁外做 IO）
func (h *Hub) NotifyTo(userIDs []int64, event any) {
	if len(userIDs) == 0 {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	// 锁内收集目标连接快照
	type target struct {
		uid int64
		c   *websocket.Conn
	}
	h.mu.Lock()
	var targets []target
	for _, uid := range userIDs {
		for c := range h.users[uid] {
			targets = append(targets, target{uid, c})
		}
	}
	h.mu.Unlock()

	if len(targets) == 0 {
		return
	}

	// 锁外并发写
	var failedMu sync.Mutex
	var failed []target
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := t.c.Write(ctx, websocket.MessageText, data); err != nil {
				failedMu.Lock()
				failed = append(failed, t)
				failedMu.Unlock()
			}
		}(t)
	}
	wg.Wait()

	// 清理失败连接
	if len(failed) > 0 {
		h.mu.Lock()
		for _, t := range failed {
			t.c.Close(websocket.StatusInternalError, "write failed")
			if conns, ok := h.users[t.uid]; ok {
				delete(conns, t.c)
				if len(conns) == 0 {
					delete(h.users, t.uid)
				}
			}
		}
		h.mu.Unlock()
	}
}

// NotifyConversation 通知会话成员（可排除发起者）
func (h *Hub) NotifyConversation(memberIDs []int64, excludeID int64, conv *Conversation) {
	ev := map[string]any{
		"type": "conversation",
		"data": conv,
	}
	ids := make([]int64, 0, len(memberIDs))
	for _, uid := range memberIDs {
		if uid != excludeID {
			ids = append(ids, uid)
		}
	}
	h.NotifyTo(ids, ev)
}

// handleWS 建立 WebSocket 连接。GET /api/ws?token=xxx
func handleWS(w http.ResponseWriter, r *http.Request) {
	u, err := userFromRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "未登录或登录已过期")
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	hub.register(u.ID, c)
	defer func() {
		hub.unregister(u.ID, c)
		c.Close(websocket.StatusNormalClosure, "")
	}()

	log.Printf("用户 %s(#%d) WebSocket 已连接", u.Username, u.ID)
	ctx := r.Context()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		var msg struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.Type == "ping" {
			writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			if err := c.Write(writeCtx, websocket.MessageText, []byte(`{"type":"pong"}`)); err != nil {
				cancel()
				break
			}
			cancel()
		}
	}
}
