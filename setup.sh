#!/bin/bash
set -e

SERVICE_NAME="s3-upload"
INSTALL_DIR="/opt/s3-upload-server"
CONFIG_DIR="/opt/s3-upload-server"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
RUN_USER="s3-upload"

# -----------------------------------------------
# 権限チェック
# -----------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
  echo "このスクリプトは root または sudo で実行してください。" >&2
  exit 1
fi

# -----------------------------------------------
# 最新バージョンを取得
# -----------------------------------------------
echo "最新バージョンを取得しています..."
VERSION=$(curl -fsSL "https://api.github.com/repos/KuronekoServer/s3-upload/releases/latest" \
  | grep '"tag_name"' | head -1 \
  | sed 's/.*"tag_name" *: *"\([^"]*\)".*/\1/')
if [ -z "${VERSION}" ]; then
  echo "最新バージョンの取得に失敗しました。" >&2
  exit 1
fi
echo "      最新バージョン: ${VERSION}"

# -----------------------------------------------
# アーキテクチャ検出
# -----------------------------------------------
ARCH=$(uname -m)
case "${ARCH}" in
  x86_64)          GOARCH="amd64" ;;
  aarch64|arm64)   GOARCH="arm64" ;;
  armv7l|armv6l)   GOARCH="arm" ;;
  i386|i686)       GOARCH="386" ;;
  riscv64)         GOARCH="riscv64" ;;
  ppc64le)         GOARCH="ppc64le" ;;
  s390x)           GOARCH="s390x" ;;
  *)
    echo "サポートされていないアーキテクチャです: ${ARCH}" >&2
    exit 1
    ;;
esac

# -----------------------------------------------
# ダウンロード
# -----------------------------------------------
DOWNLOAD_URL="https://github.com/KuronekoServer/s3-upload/releases/download/${VERSION}/s3-upload-linux-${GOARCH}.tar.gz"
echo "[1/5] リリースからバイナリをダウンロードしています (${VERSION}, linux/${GOARCH})..."
TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT
curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/s3-upload.tar.gz"
tar -xzf "${TMP_DIR}/s3-upload.tar.gz" -C "${TMP_DIR}"
BINARY=$(find "${TMP_DIR}" -maxdepth 2 -type f ! -name "*.tar.gz" | head -1)
chmod +x "${BINARY}"
echo "      ダウンロード完了: ${BINARY}"

# -----------------------------------------------
# ユーザー作成
# -----------------------------------------------
echo "[2/5] サービスユーザーを確認しています..."
if ! id -u "${RUN_USER}" &>/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${RUN_USER}"
  echo "      ユーザー '${RUN_USER}' を作成しました。"
else
  echo "      ユーザー '${RUN_USER}' は既に存在します。"
fi

# -----------------------------------------------
# バイナリをインストール
# -----------------------------------------------
echo "[3/5] バイナリをインストールしています..."
mkdir -p "${INSTALL_DIR}"
install -o root -g "${RUN_USER}" -m 750 "${BINARY}" "${INSTALL_DIR}/${SERVICE_NAME}"
echo "      インストール先: ${INSTALL_DIR}/${SERVICE_NAME}"

# -----------------------------------------------
# 設定ファイル (.env) を作成
# -----------------------------------------------
echo "[4/5] 設定ディレクトリを準備しています..."
mkdir -p "${CONFIG_DIR}"

# .env に不足しているキーを末尾に追記するヘルパー関数
_env_add_if_missing() {
  local key="$1"
  local line="$2"
  if ! grep -qE "^#?[[:space:]]*${key}[[:space:]]*=" "${CONFIG_DIR}/.env" 2>/dev/null; then
    echo "${line}" >> "${CONFIG_DIR}/.env"
    echo "      追加: ${line}"
    return 0
  fi
  return 1
}

if [ ! -f "${CONFIG_DIR}/.env" ]; then
  # 認証キーをランダム生成
  AUTH_KEY=$(openssl rand -hex 32 2>/dev/null \
    || head -c 32 /dev/urandom | od -A n -t x1 | tr -d ' \n')

  cat > "${CONFIG_DIR}/.env" <<EOF
# S3 接続設定 (必須)
S3_ACCESS_KEY=your-access-key
S3_SECRET_KEY=your-secret-key
S3_ENDPOINT=https://your-s3-endpoint
S3_BUCKET=your-bucket-name

# S3 リージョン (省略時: us-east-1)
S3_REGION=us-east-1

# サーバーポート (省略時: 8080)
PORT=8080

# 同時アップロード上限 (省略時: 10)
# MAX_CONCURRENT_UPLOADS=10

# 1パートのサイズ MB (省略時: 32、最小: 5)
# UPLOAD_PART_SIZE_MB=32

# 1ファイルあたりの並列パート数 (省略時: 8)
# UPLOAD_CONCURRENCY=8

# ヘッダー認証 (true にすると X-Auth-Key ヘッダーが必須になります)
AUTH_ENABLED=true
AUTH_KEY=${AUTH_KEY}
EOF
  chown root:"${RUN_USER}" "${CONFIG_DIR}/.env"
  chmod 640 "${CONFIG_DIR}/.env"
  # ディレクトリ自体もサービスユーザーが読めるよう設定
  chown root:"${RUN_USER}" "${CONFIG_DIR}"
  chmod 750 "${CONFIG_DIR}"
  echo "      設定ファイルを作成しました: ${CONFIG_DIR}/.env"
  echo "      生成された認証キー: ${AUTH_KEY}"
  echo "      *** 起動前に ${CONFIG_DIR}/.env を編集して認証情報を設定してください。 ***"
else
  echo "      設定ファイルは既に存在します。不足しているキーを確認しています..."
  ADDED=0
  _env_add_if_missing "S3_ACCESS_KEY"          "S3_ACCESS_KEY=your-access-key"        && ADDED=$((ADDED+1))
  _env_add_if_missing "S3_SECRET_KEY"          "S3_SECRET_KEY=your-secret-key"        && ADDED=$((ADDED+1))
  _env_add_if_missing "S3_ENDPOINT"            "S3_ENDPOINT=https://your-s3-endpoint" && ADDED=$((ADDED+1))
  _env_add_if_missing "S3_BUCKET"              "S3_BUCKET=your-bucket-name"           && ADDED=$((ADDED+1))
  _env_add_if_missing "S3_REGION"              "S3_REGION=us-east-1"                  && ADDED=$((ADDED+1))
  _env_add_if_missing "PORT"                   "PORT=8080"                            && ADDED=$((ADDED+1))
  _env_add_if_missing "MAX_CONCURRENT_UPLOADS" "# MAX_CONCURRENT_UPLOADS=10"          && ADDED=$((ADDED+1))
  _env_add_if_missing "UPLOAD_PART_SIZE_MB"    "# UPLOAD_PART_SIZE_MB=32"             && ADDED=$((ADDED+1))
  _env_add_if_missing "UPLOAD_CONCURRENCY"     "# UPLOAD_CONCURRENCY=8"               && ADDED=$((ADDED+1))
  _env_add_if_missing "AUTH_ENABLED"           "AUTH_ENABLED=true"                    && ADDED=$((ADDED+1))
  if ! grep -qE "^#?[[:space:]]*AUTH_KEY[[:space:]]*=" "${CONFIG_DIR}/.env" 2>/dev/null; then
    NEW_AUTH_KEY=$(openssl rand -hex 32 2>/dev/null \
      || head -c 32 /dev/urandom | od -A n -t x1 | tr -d ' \n')
    echo "AUTH_KEY=${NEW_AUTH_KEY}" >> "${CONFIG_DIR}/.env"
    echo "      追加: AUTH_KEY=<generated>"
    ADDED=$((ADDED+1))
  fi
  if [ "${ADDED}" -gt 0 ]; then
    echo "      ${ADDED} 件のキーを追加しました。"
    echo "      *** 追加されたキーを確認・設定してください: ${CONFIG_DIR}/.env ***"
  else
    echo "      設定ファイルは最新です。"
  fi
fi

# -----------------------------------------------
# systemd サービスファイルを作成
# -----------------------------------------------
echo "[5/5] systemd サービスファイルを作成しています..."
cat > "${SERVICE_FILE}" <<EOF
[Unit]
Description=S3 Upload Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
EnvironmentFile=${CONFIG_DIR}/.env
ExecStart=${INSTALL_DIR}/${SERVICE_NAME}
Restart=on-failure
RestartSec=5s

# セキュリティ強化
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/tmp

[Install]
WantedBy=multi-user.target
EOF

# -----------------------------------------------
# サービスを有効化
# -----------------------------------------------
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"

echo ""
echo "セットアップが完了しました。"
echo ""
echo "次のステップ:"
  echo "  1. /opt/s3-upload-server/.env を編集して認証情報を設定する"
echo "  2. sudo systemctl start ${SERVICE_NAME}   # サービス起動"
echo "  3. sudo systemctl status ${SERVICE_NAME}  # 状態確認"
echo "  4. sudo journalctl -u ${SERVICE_NAME} -f  # ログ確認"
