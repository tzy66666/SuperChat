---
AIGC:
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '5ca30af9-d9ad-4e02-9ccf-21d0d8f917c8'
  PropagateID: '5ca30af9-d9ad-4e02-9ccf-21d0d8f917c8'
  ReservedCode1: 'e5b87b83-538d-4838-9a24-b39da986430d'
  ReservedCode2: 'e5b87b83-538d-4838-9a24-b39da986430d'
---

# messenger

通讯软件服务端 —— 单个 Go 二进制 + 一个 SQLite 文件 + 一个 uploads 目录。

适合跑在 1 核 1G 的小 VPS 上，内存占用约 15-25MB。

## 功能

- 用户名 + 密码注册登录，JWT 鉴权（30 天有效期）
- **单聊**：一对一私聊，自动幂等（重复创建返回已有会话）
- **群聊**：建群、拉人、退群
- 文字 / 图片 / 文件消息
- 文件上传（默认 32MB 上限，随机文件名，MIME 白名单）
- 未读数 + 已读回执
- WebSocket 实时推送（新消息 / 会话变更）
- 全部数据落在一个 SQLite 文件，备份 = 打包 data 目录
- Caddy 反代自动 HTTPS

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.24+ | 纯标准库路由（Go 1.22 方法路由） |
| 数据库 | SQLite（modernc.org/sqlite） | 纯 Go 实现，无 CGO，交叉编译方便 |
| JWT | golang-jwt/v5 | HS256 |
| WebSocket | coder/websocket | 轻量 |
| 密码 | bcrypt | golang.org/x/crypto |
| HTTPS | Caddy | 自动签发证书 |

## 快速开始（本地）

```bash
# 编译
go build -o messenger .

# 启动（自动建 data/ 目录）
./messenger -listen :8080 -data ./data

# 参数
#   -listen      监听地址，默认 :8080
#   -data        数据目录，默认 ./data
#   -max-upload  单文件上传上限，默认 32MB，最大 64MB
```

## 部署到 VPS

> **端口规划**：VPS 上 443 端口已被 Xray REALITY 占用，Caddy 改用 8443 端口提供 HTTPS。
> 80 端口仅用于 Let's Encrypt 证书验证（ACME HTTP-01），不提供 API 服务。

```bash
# 1. 交叉编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o messenger-linux-amd64 .

# 2. 上传
scp messenger-linux-amd64 deploy/ root@YOUR_VPS:/tmp/deploy/

# 3. 一键部署
ssh root@YOUR_VPS "cd /tmp/deploy && bash deploy.sh chat.your-domain.com"

# 4. 防火墙放行 8443（如需）
ufw allow 8443/tcp
```

部署后访问地址为 `https://域名:8443`，WebSocket 地址为 `wss://域名:8443/api/ws`。

## API 文档

所有接口返回 JSON：`{"ok":true,"data":...}` 或 `{"ok":false,"error":"..."}`。
除注册/登录外均需 `Authorization: Bearer <token>`。

### 认证

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/register` | `{username, password, display_name?}` → `{token, user}` |
| POST | `/api/auth/login` | `{username, password}` → `{token, user}` |
| GET | `/api/me` | 当前用户信息 |
| GET | `/api/users?q=` | 搜索用户（排除自己，上限 50） |

### 会话

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/conversations` | 我的会话列表（含未读数、最后一条消息） |
| POST | `/api/conversations` | 创建会话 |
| GET | `/api/conversations/{id}` | 会话详情（含成员列表） |
| GET | `/api/conversations/{id}/messages` | 历史消息（?before_id=&limit=） |
| POST | `/api/conversations/{id}/read` | 标记已读 |
| POST | `/api/conversations/{id}/members` | 群聊拉人 `{user_ids:[...]}` |
| DELETE | `/api/conversations/{id}/members/me` | 退群 |

创建会话示例：
```json
// 单聊
{"type":"direct","user_id":2}

// 群聊
{"type":"group","name":"开发群","user_ids":[2,3,4]}
```

### 消息

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/messages` | `{conversation_id, type?, content?, file_id?}` |

- `type` 省略时按 file 的 MIME 自动推断（图片→image，其余→file）
- 文字消息上限 4000 字

### 文件

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/files` | multipart 表单，字段名 `file` → `{id, name, url, mime, size}` |
| GET | `/files/{id}/{路径}` | 下载/预览文件 |

### WebSocket

```
ws(s)://host/api/ws?token=<JWT>
```

事件：
```json
{"type":"message","data":{...Message}}
{"type":"conversation","data":{...Conversation}}
```

客户端每 60 秒发 `{"type":"ping"}`，服务端回 `{"type":"pong"}`。

## 代码结构

```
main.go            入口、路由、中间件
db.go              SQLite 初始化（WAL、外键）
auth.go            注册/登录/JWT
models.go          领域模型 + 数据查询层
conversations.go   会话 API（单聊/群聊/拉人/退群）
messages.go        消息发送
files.go           文件上传/下载
ws.go              WebSocket 实时推送
deploy/            Caddyfile、systemd、一键部署脚本
```

## 运维

- **日志**：`journalctl -u messenger -f`
- **备份**：`tar czf backup.tgz /opt/messenger/data`
- **恢复**：解压后 `systemctl restart messenger`

## 安全说明

- HTTPS 传输加密（Caddy 自动证书），数据库明文存储
- 上传文件随机文件名、MIME 白名单、`nosniff` 防执行
- systemd 以独立低权限用户运行
- JWT 密钥存放在 `data/jwt_secret`，务必随数据一起备份

> AI生成