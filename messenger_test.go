package main

// messenger 服务端自动化测试
// 运行: go test ./... -v
// 每个用例使用独立的临时数据库，互不干扰

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ============================================================
// 测试基础设施
// ============================================================

// newTestServer 启动一个带独立临时数据库的测试服务器
// （同包测试串行执行，全局 db/jwtKey 的替换与还原是安全的）
func newTestServer(t *testing.T) *testClient {
	t.Helper()
	dir := t.TempDir()

	oldCfg, oldDB, oldKey := cfg, db, jwtKey
	t.Cleanup(func() {
		d := db
		if d != nil && d != oldDB {
			_ = d.Close()
		}
		cfg, db, jwtKey = oldCfg, oldDB, oldKey
	})

	cfg = Config{DataDir: dir, Listen: "127.0.0.1:0", MaxUpload: 32 << 20}
	d, err := openDB(filepath.Join(dir, "messenger.db"))
	if err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	db = d
	jwtKey = []byte("unit-test-secret-key")

	mux := http.NewServeMux()
	routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testClient{t: t, base: srv.URL}
}

// testClient 携带登录态的 API 客户端
type testClient struct {
	t     *testing.T
	base  string
	token string
}

func (c *testClient) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("序列化请求体: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		c.t.Fatalf("构造请求: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s 请求失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// register 注册新用户并自动登录
func (c *testClient) register(username, displayName string) int64 {
	c.t.Helper()
	code, out := c.do("POST", "/api/auth/register", map[string]any{
		"username": username, "password": "pass123", "display_name": displayName,
	})
	if code != http.StatusCreated {
		c.t.Fatalf("注册 %s 失败: code=%d resp=%v", username, code, out)
	}
	data := dataOf(c.t, out)
	c.token = data["token"].(string)
	return int64(data["user"].(map[string]any)["id"].(float64))
}

// login 用已有用户登录
func (c *testClient) login(username string) {
	c.t.Helper()
	code, out := c.do("POST", "/api/auth/login", map[string]any{"username": username, "password": "pass123"})
	if code != http.StatusOK {
		c.t.Fatalf("登录 %s 失败: code=%d resp=%v", username, code, out)
	}
	c.token = dataOf(c.t, out)["token"].(string)
}

// createDirect 创建单聊，返回 (状态码, 会话ID)。201=新建，200=已存在复用
func (c *testClient) createDirect(peerID int64) (int, int64) {
	c.t.Helper()
	code, out := c.do("POST", "/api/conversations", map[string]any{"type": "direct", "user_id": peerID})
	if code != http.StatusCreated && code != http.StatusOK {
		c.t.Fatalf("创建单聊失败: code=%d resp=%v", code, out)
	}
	return code, int64(dataOf(c.t, out)["id"].(float64))
}

// sendText 发送文字消息
func (c *testClient) sendText(convID int64, content string) (int, map[string]any) {
	c.t.Helper()
	return c.do("POST", "/api/messages", map[string]any{"conversation_id": convID, "content": content})
}

// upload 上传文件（multipart），返回 (状态码, 响应)
func (c *testClient) upload(field, filename, contentType string, content []byte) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	h.Set("Content-Type", contentType)
	fw, err := mw.CreatePart(h)
	if err != nil {
		c.t.Fatalf("构造 multipart: %v", err)
	}
	_, _ = fw.Write(content)
	mw.Close()

	req, err := http.NewRequest("POST", c.base+"/api/files", &buf)
	if err != nil {
		c.t.Fatalf("构造上传请求: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("上传请求失败: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func dataOf(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	d, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", out)
	}
	return d
}

func listData(t *testing.T, out map[string]any) []any {
	t.Helper()
	l, ok := out["data"].([]any)
	if !ok {
		t.Fatalf("响应 data 不是数组: %v", out)
	}
	return l
}

// ============================================================
// 认证
// ============================================================

func TestAuthFlow(t *testing.T) {
	c := newTestServer(t)

	// 未登录访问受保护接口 → 401
	if code, _ := c.do("GET", "/api/me", nil); code != http.StatusUnauthorized {
		t.Errorf("无 token 访问 /api/me 应为 401，实际 %d", code)
	}
	// 弱密码 → 400
	if code, _ := c.do("POST", "/api/auth/register", map[string]any{"username": "u1", "password": "123"}); code != http.StatusBadRequest {
		t.Errorf("弱密码注册应为 400，实际 %d", code)
	}
	// 非法用户名（中文）→ 400
	if code, _ := c.do("POST", "/api/auth/register", map[string]any{"username": "小明", "password": "pass123"}); code != http.StatusBadRequest {
		t.Errorf("非法用户名应为 400，实际 %d", code)
	}
	// 正常注册 → 201，token 可用
	if id := c.register("alice", "Alice"); id != 1 {
		t.Errorf("首个用户 ID 应为 1，实际 %d", id)
	}
	code, out := c.do("GET", "/api/me", nil)
	if code != http.StatusOK {
		t.Errorf("注册后 me 应为 200，实际 %d", code)
	}
	if dataOf(t, out)["username"] != "alice" {
		t.Errorf("me 应返回 alice，实际 %v", out["data"])
	}
	// 重复注册 → 409
	if code, _ := c.do("POST", "/api/auth/register", map[string]any{"username": "alice", "password": "pass123"}); code != http.StatusConflict {
		t.Errorf("重复注册应为 409，实际 %d", code)
	}
	// 错误密码登录 → 401
	if code, _ := c.do("POST", "/api/auth/login", map[string]any{"username": "alice", "password": "wrong9"}); code != http.StatusUnauthorized {
		t.Errorf("错误密码应为 401，实际 %d", code)
	}
	// 正确登录 → token 可用
	c.login("alice")
	if code, _ := c.do("GET", "/api/me", nil); code != http.StatusOK {
		t.Errorf("登录后 me 应为 200，实际 %d", code)
	}
}

// ============================================================
// 单聊：幂等 + 并发竞态
// ============================================================

func TestDirectConversationIdempotent(t *testing.T) {
	c := newTestServer(t)
	aliceID := c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	c.login("alice") // 切回操作主体

	// 首次创建 → 201
	code1, conv1 := c.createDirect(bobID)
	if code1 != http.StatusCreated {
		t.Errorf("首次创建单聊应为 201，实际 %d", code1)
	}
	// 重复创建 → 200 且同一会话
	code2, conv2 := c.createDirect(bobID)
	if code2 != http.StatusOK || conv2 != conv1 {
		t.Errorf("重复创建应为 200 且同会话，实际 code=%d conv=%d（期望 %d）", code2, conv2, conv1)
	}
	// 对方反向创建也是同一会话
	bob := &testClient{t: t, base: c.base}
	bob.login("bob")
	_, conv3 := bob.createDirect(aliceID)
	if conv3 != conv1 {
		t.Errorf("反向创建应复用同一会话，实际 %d（期望 %d）", conv3, conv1)
	}
	// 与自己创建 → 400
	if code, _ := c.do("POST", "/api/conversations", map[string]any{"type": "direct", "user_id": aliceID}); code != http.StatusBadRequest {
		t.Errorf("与自己单聊应为 400，实际 %d", code)
	}
	// 与不存在的用户创建 → 404
	if code, _ := c.do("POST", "/api/conversations", map[string]any{"type": "direct", "user_id": 9999}); code != http.StatusNotFound {
		t.Errorf("不存在用户应为 404，实际 %d", code)
	}
}

// TestConcurrentDirectCreate 并发创建同一单聊：验证事务内"查找+创建"无竞态，
// 16 个并发请求必须全部拿到同一个会话，且数据库只有一条会话记录
func TestConcurrentDirectCreate(t *testing.T) {
	c := newTestServer(t)
	c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	c.login("alice") // 切回操作主体

	const n = 16
	ids := make([]int64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"type": "direct", "user_id": bobID})
			req, _ := http.NewRequest("POST", c.base+"/api/conversations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+c.token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("并发请求 %d 失败: %v", i, err)
				return
			}
			defer resp.Body.Close()
			var out map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&out)
			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
				t.Errorf("并发请求 %d 返回 %d: %v", i, resp.StatusCode, out)
				return
			}
			ids[i] = int64(out["data"].(map[string]any)["id"].(float64))
		}(i)
	}
	wg.Wait()

	for i, id := range ids {
		if id != ids[0] {
			t.Errorf("并发创建产生不同会话: 请求0=%d 请求%d=%d", ids[0], i, id)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM conversations WHERE type='direct'`).Scan(&count); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 1 {
		t.Errorf("并发创建后 direct 会话应为 1 个，实际 %d 个", count)
	}
}

// ============================================================
// 消息
// ============================================================

func TestMessageFlow(t *testing.T) {
	c := newTestServer(t)
	c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	c.login("alice") // 切回操作主体
	_, conv := c.createDirect(bobID)

	// 发送 → 201，带 sender 信息（客户端气泡头像依赖）
	code, out := c.sendText(conv, "你好，鲍勃")
	if code != http.StatusCreated {
		t.Fatalf("发消息应为 201，实际 %d resp=%v", code, out)
	}
	m := dataOf(t, out)
	if m["sender_name"] != "Alice" {
		t.Errorf("sender_name 应为 Alice，实际 %v", m["sender_name"])
	}
	if m["sender_avatar"] != "" {
		t.Errorf("sender_avatar 应为空字符串，实际 %v", m["sender_avatar"])
	}

	// 空内容 → 400
	if code, _ := c.sendText(conv, ""); code != http.StatusBadRequest {
		t.Errorf("空消息应为 400，实际 %d", code)
	}
	// 超长内容（>4000 字）→ 400
	if code, _ := c.sendText(conv, strings.Repeat("长", 4001)); code != http.StatusBadRequest {
		t.Errorf("超长消息应为 400，实际 %d", code)
	}
	// 非成员发消息 → 403
	c.register("carol", "Carol")
	if code, _ := c.sendText(conv, "打扰了"); code != http.StatusForbidden {
		t.Errorf("非成员发消息应为 403，实际 %d", code)
	}

	// 对方拉取历史：1 条，内容与发送者正确
	bob := &testClient{t: t, base: c.base}
	bob.login("bob")
	code, out = bob.do("GET", fmt.Sprintf("/api/conversations/%d/messages", conv), nil)
	if code != http.StatusOK {
		t.Fatalf("拉取历史应为 200，实际 %d", code)
	}
	msgs := listData(t, out)
	if len(msgs) != 1 {
		t.Fatalf("历史消息应为 1 条，实际 %d", len(msgs))
	}
	m2 := msgs[0].(map[string]any)
	if m2["content"] != "你好，鲍勃" || m2["sender_name"] != "Alice" {
		t.Errorf("消息内容/发送者不匹配: %v", m2)
	}
}

// ============================================================
// 群聊生命周期
// ============================================================

func TestGroupLifecycle(t *testing.T) {
	c := newTestServer(t)
	c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	carolID := c.register("carol", "Carol")
	c.register("david", "David")
	c.login("alice") // 切回操作主体

	// 空成员建群 → 400
	if code, _ := c.do("POST", "/api/conversations", map[string]any{"type": "group", "name": "测试群", "user_ids": []any{}}); code != http.StatusBadRequest {
		t.Errorf("空成员建群应为 400，实际 %d", code)
	}
	// 正常建群 → 201
	code, out := c.do("POST", "/api/conversations", map[string]any{"type": "group", "name": "测试群", "user_ids": []any{bobID}})
	if code != http.StatusCreated {
		t.Fatalf("建群应为 201，实际 %d resp=%v", code, out)
	}
	group := int64(dataOf(t, out)["id"].(float64))
	addPath := fmt.Sprintf("/api/conversations/%d/members", group)

	// 拉人 → 200
	if code, _ := c.do("POST", addPath, map[string]any{"user_ids": []any{carolID}}); code != http.StatusOK {
		t.Errorf("拉人应为 200，实际 %d", code)
	}
	// 重复拉已在群的成员 → 400（服务端去重）
	if code, _ := c.do("POST", addPath, map[string]any{"user_ids": []any{carolID}}); code != http.StatusBadRequest {
		t.Errorf("重复拉人应为 400，实际 %d", code)
	}
	// 非成员拉人 → 403
	david := &testClient{t: t, base: c.base}
	david.login("david")
	if code, _ := david.do("POST", addPath, map[string]any{"user_ids": []any{bobID}}); code != http.StatusForbidden {
		t.Errorf("非成员拉人应为 403，实际 %d", code)
	}
	// 群详情成员数 = 3（含群主）
	code, out = c.do("GET", fmt.Sprintf("/api/conversations/%d", group), nil)
	if code != http.StatusOK {
		t.Fatalf("群详情应为 200，实际 %d", code)
	}
	members, _ := dataOf(t, out)["members"].([]any)
	if len(members) != 3 {
		t.Errorf("群成员应为 3，实际 %d", len(members))
	}
	// 单聊不能拉人 → 400
	_, directConv := c.createDirect(bobID)
	if code, _ := c.do("POST", fmt.Sprintf("/api/conversations/%d/members", directConv), map[string]any{"user_ids": []any{carolID}}); code != http.StatusBadRequest {
		t.Errorf("单聊拉人应为 400，实际 %d", code)
	}

	// 退群 → 200；退单聊 → 400；退群后发消息 → 403
	bob := &testClient{t: t, base: c.base}
	bob.login("bob")
	if code, _ := bob.do("DELETE", fmt.Sprintf("/api/conversations/%d/members/me", group), nil); code != http.StatusOK {
		t.Errorf("退群应为 200，实际 %d", code)
	}
	if code, _ := bob.do("DELETE", fmt.Sprintf("/api/conversations/%d/members/me", directConv), nil); code != http.StatusBadRequest {
		t.Errorf("单聊退出应为 400，实际 %d", code)
	}
	if code, _ := bob.sendText(group, "我退了"); code != http.StatusForbidden {
		t.Errorf("退群后发消息应为 403，实际 %d", code)
	}
}

// ============================================================
// 通讯录
// ============================================================

func TestContacts(t *testing.T) {
	c := newTestServer(t)
	c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	carolID := c.register("carol", "Carol")
	c.register("david", "David") // 与 alice 无任何交集
	c.login("alice") // 切回操作主体

	c.createDirect(bobID)
	c.do("POST", "/api/conversations", map[string]any{"type": "group", "name": "群", "user_ids": []any{carolID}})

	code, out := c.do("GET", "/api/contacts", nil)
	if code != http.StatusOK {
		t.Fatalf("通讯录应为 200，实际 %d", code)
	}
	got := map[string]bool{}
	for _, it := range listData(t, out) {
		u := it.(map[string]any)
		got[u["username"].(string)] = true
	}
	if !got["bob"] || !got["carol"] {
		t.Errorf("通讯录应含 bob 和 carol，实际 %v", got)
	}
	if got["david"] {
		t.Errorf("通讯录不应含无交集的 david")
	}

	// david 的通讯录应为空
	david := &testClient{t: t, base: c.base}
	david.login("david")
	_, out = david.do("GET", "/api/contacts", nil)
	if l := listData(t, out); len(l) != 0 {
		t.Errorf("david 通讯录应为空，实际 %d 人", len(l))
	}
}

// ============================================================
// 用户搜索
// ============================================================

func TestSearchUsers(t *testing.T) {
	c := newTestServer(t)
	c.register("zhang1", "张一")
	c.register("zhang2", "张二")
	c.register("lisi", "李四")
	c.register("wa", "搜索者")

	// 搜索 zhang → 2 人
	code, out := c.do("GET", "/api/users?q=zhang", nil)
	if code != http.StatusOK {
		t.Fatalf("搜索应为 200，实际 %d", code)
	}
	if l := listData(t, out); len(l) != 2 {
		t.Errorf("搜索 zhang 应返回 2 人，实际 %d", len(l))
	}
	// 搜索自己 → 被排除
	_, out = c.do("GET", "/api/users?q=wa", nil)
	if l := listData(t, out); len(l) != 0 {
		t.Errorf("搜索应排除自己，实际 %d 人", len(l))
	}
}

// ============================================================
// 文件：上传 → 消息引用 → 下载，含归属校验
// ============================================================

func TestFileFlow(t *testing.T) {
	c := newTestServer(t)
	c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	c.login("alice") // 切回操作主体
	_, conv := c.createDirect(bobID)

	// 上传 PNG → 201
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4, 5, 6, 7, 8}
	code, out := c.upload("file", "test.png", "image/png", png)
	if code != http.StatusCreated {
		t.Fatalf("上传应为 201，实际 %d resp=%v", code, out)
	}
	f := dataOf(t, out)
	fileID := int64(f["id"].(float64))
	if f["mime"] != "image/png" {
		t.Errorf("mime 应为 image/png，实际 %v", f["mime"])
	}

	// 引用文件发消息 → 类型自动推断为 image，且带文件信息
	code, out = c.do("POST", "/api/messages", map[string]any{"conversation_id": conv, "file_id": fileID})
	if code != http.StatusCreated {
		t.Fatalf("发图片消息应为 201，实际 %d resp=%v", code, out)
	}
	m := dataOf(t, out)
	if m["type"] != "image" {
		t.Errorf("消息类型应自动推断为 image，实际 %v", m["type"])
	}
	if m["file"] == nil {
		t.Errorf("媒体消息应携带文件信息: %v", m)
	}

	// 非上传者引用他人文件 → 403
	bob := &testClient{t: t, base: c.base}
	bob.login("bob")
	if code, _ := bob.do("POST", "/api/messages", map[string]any{"conversation_id": conv, "file_id": fileID}); code != http.StatusForbidden {
		t.Errorf("引用他人文件应为 403，实际 %d", code)
	}

	// 通过 URL 下载：内容与上传一致
	resp, err := http.Get(c.base + f["url"].(string))
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, png) {
		t.Errorf("下载内容与上传不一致")
	}
}

// ============================================================
// 未读数
// ============================================================

func TestUnreadCount(t *testing.T) {
	c := newTestServer(t)
	c.register("alice", "Alice")
	bobID := c.register("bob", "Bob")
	c.login("alice") // 切回操作主体
	_, conv := c.createDirect(bobID)

	c.sendText(conv, "第一条")
	c.sendText(conv, "第二条")

	bob := &testClient{t: t, base: c.base}
	bob.login("bob")

	// bob 未读 = 2
	_, out := bob.do("GET", "/api/conversations", nil)
	unread := -1
	for _, it := range listData(t, out) {
		cv := it.(map[string]any)
		if int64(cv["id"].(float64)) == conv {
			unread = int(cv["unread"].(float64))
		}
	}
	if unread != 2 {
		t.Errorf("未读数应为 2，实际 %d", unread)
	}

	// 标记已读 → 0
	if code, _ := bob.do("POST", fmt.Sprintf("/api/conversations/%d/read", conv), nil); code != http.StatusOK {
		t.Errorf("标记已读应为 200，实际 %d", code)
	}
	_, out = bob.do("GET", "/api/conversations", nil)
	for _, it := range listData(t, out) {
		cv := it.(map[string]any)
		if int64(cv["id"].(float64)) == conv {
			if u := int(cv["unread"].(float64)); u != 0 {
				t.Errorf("已读后未读应为 0，实际 %d", u)
			}
		}
	}
}
