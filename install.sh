#!/usr/bin/env sh
# Aruzor kurulum betigi.
#
# Depo icinden:
#   ./install.sh
#
# Sifirdan bir sunucuda tek komutla:
#   curl -fsSL https://raw.githubusercontent.com/eyuphan-dev/aruzor/main/install.sh | sh
#
# Docker Compose yiginini ayaga kaldirir. Calistirmadan once eksik olan
# sirlari uretir: her yeniden baslatmada oturumlari dusurmeyecek bir imza
# anahtari ve tahmin edilemeyecek bir yonetici sifresi. Betik yeniden
# calistirildiginda mevcut degerlere dokunmaz, yigini gunceller.

set -eu

REPO_URL="${ARUZOR_REPO:-https://github.com/eyuphan-dev/aruzor.git}"
TARGET_DIR="${ARUZOR_DIR:-aruzor}"

say() { printf '%s\n' "$*"; }
fail() { printf 'HATA: %s\n' "$*" >&2; exit 1; }

# Piped straight from curl there is no script directory to stand in, and
# "$0" is the shell itself. Detect that by looking for the compose file next
# to the script rather than by inspecting "$0", which lies in that case.
SCRIPT_DIR=$(dirname "$0" 2>/dev/null || echo ".")
if [ -f "$SCRIPT_DIR/docker-compose.yml" ]; then
  cd "$SCRIPT_DIR"
else
  command -v git >/dev/null 2>&1 || fail "git bulunamadi. Once git kurun: apt install git"
  if [ -d "$TARGET_DIR/.git" ]; then
    say "Mevcut kurulum guncelleniyor: $TARGET_DIR"
    cd "$TARGET_DIR"
    git pull --ff-only
  else
    say "Depo indiriliyor: $TARGET_DIR"
    git clone --depth 1 "$REPO_URL" "$TARGET_DIR"
    cd "$TARGET_DIR"
  fi
fi

ENV_FILE=".env"
PORT="${ARUZOR_PORT:-3000}"

# --- Ön koşullar ------------------------------------------------------------

if ! command -v docker >/dev/null 2>&1; then
  fail "docker bulunamadi. Kurmak icin:  curl -fsSL https://get.docker.com | sh"
fi

if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  fail "docker compose bulunamadı. Docker Compose eklentisini kurun."
fi

# --- Sırlar -----------------------------------------------------------------

# Rastgeleliği openssl'e bağlamamak için: her Linux'ta /dev/urandom var,
# openssl her zaman kurulu değil.
random_hex() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$1"
  else
    head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

if [ ! -f "$ENV_FILE" ]; then
  say "Yapılandırma oluşturuluyor: $ENV_FILE"
  {
    echo "# Aruzor yapılandırması. Bu dosyayı gizli tutun."
    echo "ARUZOR_PORT=$PORT"
    echo ""
    echo "# Yönetici hesabı tarayıcıdaki kurulum ekranından oluşturulur."
    echo "# Buraya bir şifre yazarsanız hesap doğrudan o şifreyle kurulur"
    echo "# (etkileşimsiz kurulumlar için)."
    echo "ARUZOR_ADMIN_EMAIL="
    echo "ARUZOR_ADMIN_PASSWORD="
    echo ""
    echo "# Oturum imza anahtarı. Değişirse herkes çıkış yapmış olur."
    echo "ARUZOR_JWT_SECRET=$(random_hex 32)"
    echo "ARUZOR_TIMEZONE=Europe/Istanbul"
    echo ""
    echo "# Telegram bildirimleri (isteğe bağlı)"
    echo "ARUZOR_TELEGRAM_BOT_TOKEN="
    echo "ARUZOR_TELEGRAM_CHAT_ID="
  } > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  NEW_INSTALL=1
else
  say "Mevcut $ENV_FILE kullanılıyor."
  NEW_INSTALL=0
fi

# --- Kurulum ----------------------------------------------------------------

say "Konteynerler hazırlanıyor (ilk seferde birkaç dakika sürebilir)..."
$COMPOSE up -d --build

say ""
say "Servisler kontrol ediliyor..."
# Sağlıklı bir yanıt gelene kadar bekle: derleme bitmiş olsa da Next.js'in
# dinlemeye başlaması birkaç saniye sürüyor ve hemen basılan bir adres
# kullanıcıyı boş sayfaya götürüyor.
i=0
while [ "$i" -lt 60 ]; do
  if curl -fsS "http://localhost:${PORT}/api/v1/health" >/dev/null 2>&1; then
    READY=1
    break
  fi
  i=$((i + 1))
  sleep 2
done

ADDRESS="http://$(hostname -I 2>/dev/null | awk '{print $1}'):${PORT}"
[ "$ADDRESS" = "http://:${PORT}" ] && ADDRESS="http://localhost:${PORT}"

say ""
if [ "${READY:-0}" = "1" ]; then
  say "✅ Aruzor çalışıyor: $ADDRESS"
else
  say "⚠️  Servisler hâlâ başlıyor olabilir. Birkaç dakika sonra $ADDRESS adresini deneyin."
  say "    Kayıtlar:  $COMPOSE logs -f"
fi

if [ "$NEW_INSTALL" = "1" ]; then
  say ""
  say "Adresi açın ve yönetici hesabınızı oluşturun — şifreyi siz belirlersiniz."
  say ""
  say "Ardından Prometheus'un topladığı veriler taranır ve uygun paneller"
  say "önerilir; sorgu yazmanıza gerek yoktur."
fi
