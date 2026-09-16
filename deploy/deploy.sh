#!/bin/bash
# 一键部署脚本
# 用法: bash deploy.sh chat.your-domain.com
set -e

DOMAIN="${1:?用法: bash deploy.sh <域名>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== messenger 部署开始 ==="
echo "域名: $DOMAIN"

# 1. 创建系统用户
if ! id -u messenger &>/dev/null; then
    useradd -r -s /usr/sbin/nologin -d /opt/messenger messenger
    echo "✓ 创建用户 messenger"
fi

# 2. 创建目录
mkdir -p /opt/messenger/data/uploads
chown -R messenger:messenger /opt/messenger
echo "✓ 创建目录 /opt/messenger"

# 3. 安装二进制
cp "$SCRIPT_DIR/../messenger-linux-amd64" /opt/messenger/messenger
chmod +x /opt/messenger/messenger
chown messenger:messenger /opt/messenger/messenger
echo "✓ 安装二进制"

# 4. 安装 systemd 服务
cp "$SCRIPT_DIR/messenger.service" /etc/systemd/system/messenger.service
systemctl daemon-reload
systemctl enable messenger
systemctl restart messenger
echo "✓ 启动 systemd 服务"

# 5. 配置 Caddy
CADDYFILE="/etc/caddy/Caddyfile"
if [ ! -f "$CADDYFILE" ]; then
    echo "" > "$CADDYFILE"
fi

# 移除旧的 messenger 配置块
sed -i "/# BEGIN messenger/,/# END messenger/d" "$CADDYFILE"

cat >> "$CADDYFILE" <<EOF
# BEGIN messenger
$DOMAIN:8443 {
    reverse_proxy 127.0.0.1:8080
    request_body {
        max_size 64MB
    }
    encode gzip zstd
}
$DOMAIN:80 {
    respond "messenger API" 200
}
# END messenger
EOF
echo "✓ 配置 Caddy ($DOMAIN:8443)"

# 6. 重载 Caddy
if systemctl is-active --quiet caddy; then
    systemctl reload caddy
    echo "✓ 重载 Caddy"
else
    systemctl enable caddy
    systemctl start caddy
    echo "✓ 启动 Caddy"
fi

# 7. 等待服务就绪
sleep 2
if systemctl is-active --quiet messenger; then
    echo ""
    echo "=== 部署完成 ==="
    echo "API 地址: https://$DOMAIN:8443"
    echo "验证: curl https://$DOMAIN:8443/api/me (应返回 401)"
    echo "日志: journalctl -u messenger -f"
else
    echo "✗ messenger 服务启动失败，请检查: journalctl -u messenger -e"
    exit 1
fi
