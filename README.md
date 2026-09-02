<div align="center">

<img src="frontend/public/logo-mark.png" alt="Aruzor" width="96" />

# Aruzor

**Prometheus üzerine kurulu, sorgu yazmadan kullanılan sistem izleme platformu.**

Sunucularınızı izleyin, eşik aşıldığında Telegram'dan haberdar olun, servislerinizin
ayakta olup olmadığını takip edin — hiç PromQL yazmadan.

Türkçe / English · Açık ve koyu tema · Telefona kurulabilir (PWA)

</div>

---

## Neden

Prometheus güçlü, ama önüne bir arayüz koymak genelde şu anlama geliyor: Grafana kur,
veri kaynağı tanımla, panel oluştur, her panel için PromQL yaz. İzlemek istediğin şey
"sunucum ayakta mı, diski doluyor mu" kadar basitse bu yolun tamamı fazladan iş.

Aruzor bu adımları atlar. Kurduğunuz anda Prometheus'un ne topladığına bakar, tanıdığı
exporter'lar için hazır panelleri önerir ve çalışan bir gösterge paneliyle açılır.

## Öne çıkanlar

| | |
| :-- | :-- |
| **Otomatik keşif** | İlk açılışta Prometheus'un topladığı metrikler taranır. Node Exporter, PostgreSQL, MySQL, Redis, Nginx, cAdvisor, Blackbox ve Kubernetes tanınır; uygun paneller tek tıkla eklenir. |
| **Sorgu oluşturucu** | Metrik seç, filtrele, grafiği gör. PromQL bilmek gerekmez — ama isterseniz üretilen sorguyu görüp elle düzenleyebilirsiniz. |
| **Sürüklenebilir paneller** | Çizgi, alan, sütun, pasta, sayaç ve gösterge tipleri. Panel başlığından tip değiştirilir, CSV indirilir, tam ekran açılır. |
| **Alarm kuralları** | Bir eşik aşıldığında Telegram'dan anlık bildirim; değişiklik olmazsa günde bir özet. Bakım için susturulabilir. |
| **Servis izleme** | Prometheus'tan bağımsız HTTP/TCP kontrolleri. Kesinti olduğunda sebebi de söylenir: sertifika mı, güvenlik duvarı mı, sunucu mu, uygulama mı. |
| **Trafik analizi** | Web sunucusunun erişim logundan: istek hızı, giden bant genişliği, domain ve servis kırılımı, en çok istek atan ve en çok bant tüketen IP'ler, en çok istenen yollar, istemciler, 5xx hatalar, yetkisiz denemeler ve son istekler. |
| **Sertifika uyarısı** | SSL sertifikasının süresi dolmasına 7 gün kalınca Telegram/webhook/tarayıcı bildirimi gider, izleme sayfasında 14 gün kala görünür. Her sertifika için tek mesaj; yenilenince kendiliğinden sıfırlanır. |
| **Bakım penceresi** | Planlı bir kesinti öncesi bildirimleri geçici olarak durdurun — kontrol arka planda çalışmaya devam eder, geçmiş kaydı bozulmaz. |
| **Çalışma süresi raporu** | 24 saat / 7 gün / 30 gün / 90 gün yüzdeleri ve günlük geçmiş şeridi — hem panelde hem herkese açık durum sayfasında. |
| **Tarayıcı bildirimi** | Telegram açık olmasa bile, kesinti anında telefonun kilit ekranına bildirim düşer. Kurulum gerektirmez. |
| **Webhook / Slack / Discord** | Her alarm ve kesinti isteğe bağlı bir adrese de JSON olarak gönderilir — kendi entegrasyonunuzu kurun. |
| **Panel paylaşımı** | Bir gösterge panelini giriş gerektirmeyen tek kullanımlık bir bağlantıyla paylaşın. Bağlantı yalnızca o panelin sorgularını çalıştırabilir. |
| **Rol tabanlı erişim** | viewer / editor / admin / super_admin. Herkese açık kayıt yoktur; hesapları super_admin oluşturur. |
| **Telegram botu** | Düğmelerle çalışır: sistem durumu, alarmlar, izlemeler, susturma. Komut ezberlemek gerekmez. |
| **Telefonda uygulama gibi** | Alt sekme çubuğu, ana ekrana eklenebilir, çevrimdışıyken açılır ve bağlantının kopuk olduğunu söyler. |

---

## Kurulum

### Tek komut

Docker kurulu, temiz bir sunucuda:

```bash
curl -fsSL https://raw.githubusercontent.com/eyuphan-dev/aruzor/main/install.sh | sh
```

Betik depoyu indirir, gerekli sırları üretir, yığını ayağa kaldırır ve hazır olduğunda
adresi ekrana yazar. Docker yoksa nasıl kurulacağını söyler.

Depoyu zaten klonladıysanız:

```bash
git clone https://github.com/eyuphan-dev/aruzor.git && cd aruzor
./install.sh
```

Açıldığında `http://<sunucu-adresi>:3000` adresine gidip **yönetici hesabınızı
oluşturun** — şifreyi siz belirlersiniz, hiçbir yere yazılmaz.

### Farklı bir port

```bash
ARUZOR_PORT=8090 ./install.sh
```

Yalnızca bu tek port dışarı açılır. Backend ve Prometheus konteyner ağının içinde kalır,
tarayıcı yalnızca ön yüzle konuşur — güvenlik duvarında açılacak tek bir port olur.

### Yapılandırma gerekmez

- **Prometheus adresi otomatik bulunur.** Belirtmezseniz loopback, compose servis adı ve
  Docker ağ geçidi sırayla yoklanır.
- **node-exporter yığına dahildir**, yani ilk dakikadan itibaren gerçek veri vardır.
- **Kurulu exporter'lar otomatik keşfedilir** ve uygun paneller önerilir.

### Yükseltme

```bash
cd aruzor && git pull && ./install.sh
```

Betik mevcut `.env` dosyanıza ve verilerinize dokunmaz, yalnızca yığını günceller.

---

## Kendi sunucularınızı ekleme

İzlemek istediğiniz her makineye [node-exporter](https://github.com/prometheus/node_exporter)
kurun, sonra `prometheus.yml` içine hedefi ekleyin:

```yaml
  - job_name: "other-hosts"
    static_configs:
      - targets: ["10.0.0.2:9100", "10.0.0.3:9100"]
```

```bash
docker compose restart prometheus
```

Aruzor her seriyi makinesine göre etiketler; paneller aynı grafikte ayrı çizgiler olarak
görünür. Araç çubuğundaki sunucu seçicisiyle tek makineye süzebilirsiniz.

---

## Telegram bildirimleri

İsteğe bağlıdır, kapalıyken de her şey çalışır.

1. Telegram'da [@BotFather](https://t.me/BotFather) ile `/newbot` yazıp bir bot açın, size
   verilen token'ı saklayın.
2. Botu bildirimlerin gideceği gruba ekleyin.
3. Grubun sohbet kimliğini öğrenin (bota bir mesaj atıp
   `https://api.telegram.org/bot<TOKEN>/getUpdates` adresini açın, `chat.id` alanına bakın).
4. `.env` dosyasına yazın ve yeniden başlatın:

```bash
ARUZOR_TELEGRAM_BOT_TOKEN=...
ARUZOR_TELEGRAM_CHAT_ID=-1001234567890
```

```bash
docker compose up -d
```

Bot düğmelerle çalışır — sistem durumu, alarm veren kurallar, izlemeler ve susturma.
Bildirim politikası bilinçli olarak sessizdir: kısa takılmalar için mesaj gitmez,
yalnızca bir servis **beş dakika kesintisiz** erişilemez kaldığında haber verilir. Kısa
dalgalanmalar arayüzdeki 24 saatlik sağlık şeridinde görünür.

---

## Trafik analizi

Prometheus exporter'ları bir sürecin sayaçlarını yayınlar, o sürece gelen tek
tek istekleri değil. "Şu an hangi IP bizi dövüyor", "hangi yol en çok
isteniyor", "hangi tarayıcıdan geliyor" sorularının cevabı yalnızca web
sunucusunun erişim logunda vardır — bu yüzden Trafik sayfası Prometheus'a
değil, doğrudan log dosyasına bakar.

Ek kurulum gerekmez: nginx veya Apache'nin bilinen log konumları ilk açılışta
denenir. Log dosyanız başka bir yerdeyse:

```bash
ARUZOR_ACCESS_LOG_PATHS=/www/wwwlogs/*.log
```

Standart `combined` formatı doğrudan çalışır. Domain kırılımı, servis
kırılımı ve yanıt süresi panelleri için log formatına üç alan eklemek
gerekir; Aruzor bu alanların eksik olduğunu fark eder ve ilgili panelde ne
ekleneceğini yazar:

```nginx
log_format aruzor '$remote_addr - $remote_user [$time_local] "$request" '
                  '$status $body_bytes_sent "$http_referer" "$http_user_agent" '
                  '"$host" $request_time "$upstream_addr"';

access_log /www/wwwlogs/access.log aruzor;
```

Ham satırlar veritabanına yazılmaz. Dakikalık toplamlar, her dakikanın en çok
istek alan kayıtları ve son isteklerden oluşan kısa bir kuyruk tutulur — yani
sitenin trafiği ne kadar artarsa artsın veritabanı büyümesi sınırlı kalır.
Veriler 7 gün saklanır.

Sayfa ziyaretçilerin IP adreslerini ve istedikleri adresleri içerdiği için
yalnızca **admin** ve **super_admin** rollerine açıktır.

---

## Ortam değişkenleri

Hepsi isteğe bağlıdır; `install.sh` gerekli olanları üretir.

| Değişken | Varsayılan | Açıklama |
| :--- | :--- | :--- |
| `ARUZOR_PORT` | `3000` | Dışarı açılan tek port |
| `ARUZOR_JWT_SECRET` | _(üretilir)_ | Oturum imza anahtarı. Değişirse herkes çıkış yapmış olur |
| `ARUZOR_ADMIN_EMAIL` | _(boş)_ | Etkileşimsiz kurulum için. Boşsa hesap tarayıcıdaki kurulum ekranından açılır |
| `ARUZOR_ADMIN_PASSWORD` | _(boş)_ | Aynı şekilde. Boş bırakmak tercih edilir — şifre hiçbir yere yazılmaz |
| `ARUZOR_PROMETHEUS_URL` | _(otomatik)_ | Belirtilirse otomatik arama yapılmaz |
| `ARUZOR_TIMEZONE` | `Europe/Istanbul` | Günlük özetin saati bu zaman dilimine göre hesaplanır |
| `ARUZOR_DAILY_DIGEST_HOUR` | `9` | Günlük sağlık özetinin saati (0–23) |
| `ARUZOR_ALERT_EVAL_INTERVAL` | `60s` | Alarm kurallarının kontrol sıklığı |
| `ARUZOR_TELEGRAM_BOT_TOKEN` | _(boş = kapalı)_ | Bot token'ı |
| `ARUZOR_TELEGRAM_CHAT_ID` | _(boş = kapalı)_ | Bildirimlerin gideceği sohbet |
| `ARUZOR_ACCESS_LOG_PATHS` | _(otomatik)_ | Trafik analizi için erişim logu yolları; virgülle ayrılır, joker karakter ve `ad=yol` biçimi desteklenir |
| `ARUZOR_DB_PATH` | `aruzor.db` | SQLite dosya yolu |
| `ARUZOR_LISTEN_ADDR` | `:8080` | Backend'in dinlediği adres |
| `ARUZOR_CORS_ORIGIN` | _(boş)_ | Yalnızca API'yi doğrudan başka bir origin'e açarsanız gerekir |

---

## Canlıya almadan önce

- **`.env` dosyasını gizli tutun.** İmza anahtarı ve varsa bot token'ı orada durur; depoya
  eklenmez, `.gitignore` içindedir.
- **HTTPS için önüne bir ters vekil koyun** (nginx, Caddy, Traefik). Aruzor kendi başına
  TLS sunmaz.
- **Herkese açık kayıt yoktur** — bu tasarım gereğidir. Yeni hesapları super_admin açar.
- Yalnızca `ARUZOR_PORT` dışarı açık olmalı; başka port açmaya gerek yok.

---

## Mimari

```
                 tarayıcı
                    │
                    ▼
        ┌───────────────────────┐
        │  Aruzor UI (Next.js)  │   tek yayınlanan port
        │   /api → backend      │
        └───────────┬───────────┘
                    │  konteyner ağı
        ┌───────────▼───────────┐        ┌──────────────┐
        │ Aruzor Engine (Go)    │───────▶│  Prometheus  │
        │  REST API · SQLite    │        └──────┬───────┘
        │  alarm · uptime · bot │               │
        └───────────────────────┘        ┌──────▼───────┐
                                         │  exporter'lar │
                                         └──────────────┘
```

Aruzor zaman serisi verisini kendi içinde tutmaz; her sorgu anlık olarak Prometheus'a
gider. SQLite yalnızca gösterge panelleri, alarm kuralları, izlemeler ve kullanıcılar
için kullanılır — yani yedeklemesi küçük ve kolaydır.

```
aruzor/
├── backend/            Go — REST API, Prometheus istemcisi, alarm motoru,
│                       uptime denetleyicisi, Telegram botu, SQLite deposu
├── frontend/           Next.js + TypeScript + Tailwind — arayüz, PWA
├── docker-compose.yml  Aruzor + Prometheus + node-exporter
├── prometheus.yml      Toplama hedefleri
└── install.sh          Tek komutluk kurulum
```

---

## Geliştirme

Gereksinimler: Go 1.26+, Node.js 22+, erişilebilir bir Prometheus.

```bash
# Backend
cd backend
go run ./cmd/aruzor

# Frontend (ayrı bir terminalde)
cd frontend
npm install
npm run dev
```

Arayüz `http://localhost:3000`, API `http://localhost:8080` üzerinde çalışır. Backend'i
başka bir adrese almak isterseniz `frontend/.env.local` içine:

```
NEXT_PUBLIC_ARUZOR_API_URL=http://localhost:8080
```

Testler:

```bash
cd backend && go test ./...
cd frontend && npx eslint src && npx tsc --noEmit
```
