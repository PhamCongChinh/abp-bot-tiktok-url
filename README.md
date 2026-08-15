# ABP Bot TikTok URL

Bot crawl TikTok theo **danh sách URL cho sẵn** (video hoặc profile) viết bằng Go, dùng Playwright điều khiển trình duyệt thật (qua GPM/GoLogin hoặc Chrome local). Khác với `abp-bot-tiktok` (crawl theo từ khóa tìm kiếm), bot này đọc danh sách URL TikTok từ MongoDB, mở trực tiếp từng URL (không qua search), lấy dữ liệu qua network interception, rồi đẩy video thu được sang một backend API bên ngoài.

## Cấu trúc

```
├── cmd/
│   └── main.go              # Entry point: load config, connect Mongo, khởi tạo crawler/scheduler
├── internal/
│   ├── crawler/              # Orchestration crawl
│   │   ├── crawler.go         #   fan-out theo profile, chia URL, chạy chu kỳ crawl
│   │   ├── fetcher.go          #   điều hướng trực tiếp tới URL TikTok bằng Playwright
│   │   ├── scraper.go         #   helper tạo/khởi tạo page
│   │   ├── publisher.go       #   parse video thô, gom batch, đẩy sang API
│   │   ├── gpm.go              #   kết nối GPM + Playwright qua CDP, có retry (circuit breaker)
│   │   └── metrics.go          #   định nghĩa Prometheus metrics
│   ├── models/video.go        # Struct VideoItem (dữ liệu video crawl được)
│   ├── parser/tiktok_post.go  # Map VideoItem -> TiktokPost (payload gửi backend API)
│   ├── repository/            # Mongo repositories
│   │   ├── url_repo.go         #   đọc collection `tiktok_url`
│   │   ├── bot_config_repo.go  #   đọc/ghi config bot
│   │   └── video_repo.go       #   interface lưu video vào Mongo (hiện không dùng, xem ghi chú bên dưới)
│   ├── scheduler/scheduler.go # Lặp crawl mỗi 30-45 phút, tạm nghỉ 00:00-03:00
│   └── utils/                 # delay.go, scroll.go, retry.go, sysmon.go — mô phỏng hành vi người dùng
├── pkg/
│   ├── config/config.go       # Load & validate config từ env
│   ├── database/mongodb.go    # Khởi tạo Mongo client
│   ├── gpm/client.go          # HTTP client gọi GPM (GoLogin Profile Manager)
│   ├── api/client.go          # HTTP client đẩy TiktokPost sang backend API
│   └── logger/logger.go       # Zap logger có rotation
├── go.mod / go.sum
├── build.bat, bot.exe          # Build/binary Windows
├── .env.example
└── data/                       # Log output (./data/logs/bot.log)
```

## Khác biệt so với `abp-bot-tiktok` (bản search theo keyword)

| | `abp-bot-tiktok` | `abp-bot-tiktok-url` (bản này) |
| --- | --- | --- |
| Đầu vào | Từ khóa (`keyword` collection) | URL TikTok cho sẵn (`tiktok_url` collection) |
| Điều hướng | `tiktok.com/search?q=...` | Mở thẳng URL video hoặc profile |
| Nguồn dữ liệu | XHR `/api/search/item/full/` | XHR `/api/item/detail/` (video) hoặc `/api/post/item_list/` (profile) |
| Phân trang | Scroll trang search | Scroll trang profile (video đơn thì không cần) |
| Field gốc | `keyword` | `url` (models.VideoItem.SourceURL) |

Phần còn lại (GPM, circuit breaker, publisher, scheduler, anti-ban, metrics, logger) **giữ nguyên logic** so với bản search.

## Công nghệ sử dụng

- **Go 1.25**
- **Playwright-go** (`playwright-community/playwright-go`) — điều khiển Chromium/Chrome thật
- **GPM (GoLogin Profile Manager)** — quản lý profile trình duyệt anti-detect đã login sẵn, kết nối qua Chrome DevTools Protocol (CDP)
- **MongoDB** (`mongo-driver`) — nguồn URL và bot config
- **Zap + lumberjack** — structured logging có rotation
- **Prometheus client** — expose metrics tại endpoint `/metrics`
- **gopsutil** — kiểm tra CPU/RAM trước khi crawl
- **godotenv** — load cấu hình từ `.env`

Không có web framework phục vụ request (chỉ có endpoint metrics nội bộ), không dùng queue, không dùng ORM.

## Cài đặt

### 1. Cài Go (>= 1.25)

```bash
go version
```

### 2. Clone project & cài dependencies

```bash
git clone <repo-url>
cd abp-bot-tiktok-url
go mod download
```

### 3. Cài Playwright driver

```bash
go run github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 install chromium
```

### 4. Cài MongoDB

- Local: https://www.mongodb.com/try/download/community
- Hoặc dùng MongoDB Atlas (cloud)

## Cấu hình

### 1. Tạo `.env`

```bash
cp .env.example .env
```

### 2. Các biến bắt buộc

| Biến | Ý nghĩa |
| --- | --- |
| `BOT_NAME` | Tên bot instance (dùng trong bot_config Mongo) |
| `ORG_IDS` | Danh sách org_id crawl URL, cách nhau bởi dấu phẩy, vd `1,2,3` |
| `MONGO_URI` | Connection string MongoDB |
| `MONGO_DB` | Tên database |
| `GPM_API` | URL base của GPM API, vd `http://localhost:50325/api/v1` |

### 3. Các biến tuỳ chọn (có default)

```env
# GPM profile — bỏ trống để dùng Chrome local thay vì GPM
# PROFILE_IDS=profile-1,profile-2

# Backend API nhận video crawl được — bỏ trống để tắt push
# API_URL=http://localhost:8080/api

# OUTPUT_DIR=./data
# DEBUG=false                # true = chạy 1 lần ngay, false = chạy theo lịch định kỳ

# LOG_LEVEL=info
# LOG_MAX_SIZE_MB=100
# LOG_MAX_AGE_DAYS=7
# LOG_MAX_BACKUPS=7

# MONGO_MAX_POOL_SIZE=100
# MONGO_MIN_POOL_SIZE=10

# HTTP_TIMEOUT_SECONDS=30

# URLS=https://www.tiktok.com/@someuser,https://www.tiktok.com/@otheruser/video/7123456789012345678

# SLEEP_MIN_URL=60            # nghỉ giữa các URL (giây)
# SLEEP_MAX_URL=180
# REST_MIN_SESSION=180        # nghỉ giữa các phiên crawl đầy đủ (giây)
# REST_MAX_SESSION=300

# BATCH_MIN=3                  # số URL xử lý theo batch
# BATCH_MAX=5

# MAX_VIDEOS_PER_URL=200       # giới hạn video/URL (chủ yếu áp dụng cho profile URL)
# MAX_PAGES_PER_SESSION=20     # giới hạn số trang scroll/phiên

# METRICS_ADDR=:9090           # bỏ trống để tắt Prometheus endpoint
```

Xem đầy đủ và chú thích chi tiết trong [`.env.example`](.env.example).

Không cần tạo `profiles.json` — bot mở TikTok public video/profile không cần login (trừ khi dùng GPM để tăng độ ổn định, xem bên dưới).

## Sử dụng

### Chạy crawler (DEBUG mode, chạy 1 lần)

```bash
# .env: DEBUG=true
go run cmd/main.go
```

### Chạy production (lịch định kỳ)

```bash
# .env: DEBUG=false
build.bat
bot.exe
```

Khi `DEBUG=false`, bot chạy ngay một chu kỳ khi khởi động, sau đó lặp lại mỗi 30-45 phút (ngẫu nhiên), tự tạm nghỉ trong khung giờ 00:00-03:00.

## Tự khởi động cùng Windows

Để bot tự chạy mỗi khi đăng nhập Windows (không cần mở tay):

```bash
build.bat
install_startup.bat
```

`install_startup.bat` tạo shortcut trỏ tới `bot.exe` trong thư mục Startup của Windows (`shell:startup`). Bot sẽ tự chạy (kèm cửa sổ console) ngay khi bạn đăng nhập lần sau.

**Gỡ bỏ:** mở Win+R, gõ `shell:startup`, xóa shortcut `abp-bot-tiktok-url`.

**Lưu ý:** cách này không tự khởi động lại nếu bot bị crash. Nếu cần restart tự động khi lỗi, cân nhắc dùng Task Scheduler thay vì Startup folder.

## GPM Setup (GoLogin Profile Manager)

GPM cho phép sử dụng browser profile đã login TikTok sẵn, tránh phải login lại mỗi lần chạy, đồng thời cho phép chạy nhiều profile song song (mỗi profile trong `PROFILE_IDS` chạy một goroutine riêng, xử lý một phần danh sách URL).

### Cài đặt GPM

1. Download GPM: https://gologin.com/
2. Cài đặt và mở GPM
3. Tạo profile mới hoặc dùng profile có sẵn
4. Login TikTok trong profile đó

### Lấy thông tin GPM

- **API URL**: mặc định `http://localhost:50325/api/v1` (kiểm tra trong GPM Settings → API)
- **Profile ID**: mở GPM → click vào profile muốn dùng → copy Profile ID từ URL hoặc profile settings

Kiểm tra GPM đang chạy:

```bash
curl http://127.0.0.1:19995/api/v3/profiles
```

Nếu lỗi → mở GPM Login trước.

### Cách bot hoạt động khi có GPM

1. Gọi GPM API để start profile
2. Lấy remote debugging address (WebSocket CDP)
3. Connect Playwright qua CDP (Chrome DevTools Protocol)
4. Sử dụng browser đã login sẵn để mở URL TikTok
5. Tự động stop profile khi xong (có retry/backoff + circuit breaker nếu lỗi kết nối)

### Chạy không dùng GPM (local Chrome)

Để trống `PROFILE_IDS` trong `.env`:

```env
# PROFILE_IDS=
```

Bot sẽ tự động dùng local Chrome (`UseGPM=false` khi `PROFILE_IDS` rỗng).

### So sánh GPM vs Local Chrome

| | GPM | Local Chrome |
| --- | --- | --- |
| Login TikTok | ✅ Tự động (đã login sẵn) | ❌ Cần login thủ công |
| Anti-detect | ✅ Fingerprint riêng | ⚠️ Dễ bị detect |
| Multi-profile | ✅ Nhiều profile song song | ❌ Chỉ 1 profile |
| Setup | ⚠️ Cần cài GPM | ✅ Đơn giản |
| Performance | ⚠️ Hơi chậm | ✅ Nhanh |

**Khuyến nghị**: Production dùng GPM với nhiều profile đã login, Development/Test dùng local Chrome.

## Luồng hoạt động

1. **Load config & URL**: đọc `.env`, kết nối MongoDB, lấy danh sách URL đang active theo `ORG_IDS` từ collection `tiktok_url`.
2. **Fan-out theo profile**: với mỗi `PROFILE_ID` trong `PROFILE_IDS`, chạy một goroutine riêng (lệch nhau 15-45s), xử lý một phần (shard) danh sách URL.
3. **Kết nối browser**: gọi GPM API start profile đã login sẵn, lấy CDP endpoint, connect Playwright vào (fallback Chrome local nếu không cấu hình GPM).
4. **Phân loại URL**: URL chứa `/video/` → video đơn; URL chứa `/@username` (không có `/video/`) → trang profile; URL không khớp mẫu nào → bỏ qua.
5. **Crawl từng URL**: mở thẳng URL (không qua search). Chặn load ảnh/font/css để tăng tốc.
   - **Video**: lắng nghe response XHR nội bộ `/api/item/detail/`, lấy JSON gốc (`itemInfo.itemStruct`).
   - **Profile**: lắng nghe response XHR nội bộ `/api/post/item_list/` (`itemList`), scroll để trang tự động phân trang và tải thêm video.
6. **Mô phỏng hành vi người dùng**: di chuột ngẫu nhiên, scroll kiểu người thật (trang profile), xem video ngẫu nhiên, delay ngẫu nhiên — nhằm né anti-bot detection. Phát hiện mã lỗi rate-limit/captcha (2061, 10000, -1) thì tạm dừng 5 phút.
7. **Parse & dedup**: trích video ID, mô tả, thời gian đăng, tác giả, số liệu tương tác vào `models.VideoItem`; lọc video cũ hơn 7 ngày; loại trùng theo ID.
8. **Đẩy kết quả**: convert `VideoItem` → `TiktokPost`, đẩy bất đồng bộ (buffered channel + worker pool, batch tối đa 10 hoặc mỗi 5s) sang backend API tại `POST {API_URL}/api/v1/posts/insert-unclassified-org-posts`.
9. **Guardrail chống ban**: batch size, sleep ngẫu nhiên giữa URL (`SLEEP_MIN/MAX_URL`) và giữa session (`REST_MIN/MAX_SESSION`), giới hạn video/URL và số trang/session, tạm nghỉ crawl 00:00-03:00.
10. **Observability**: Prometheus counter/histogram cho số chu kỳ crawl, số video crawl được, lỗi push API, thời lượng crawl — expose tại `METRICS_ADDR` (`/metrics`).

## MongoDB Collections

### `tiktok_url` (đầu vào)

```javascript
{
  _id: ObjectId("..."),
  url: "https://www.tiktok.com/@someuser/video/7123456789012345678",
  org_id: 1,
  active: true
}
```

URL có thể là:
- **Video**: `https://www.tiktok.com/@username/video/<id>` — crawl đúng 1 video đó.
- **Profile**: `https://www.tiktok.com/@username` — crawl các video mới nhất trên trang profile (theo `MAX_VIDEOS_PER_URL`).

### Backend API nhận video (đầu ra — không ghi thẳng Mongo)

Video crawl được **không** ghi trực tiếp vào MongoDB trong luồng hiện tại — chúng được convert sang `TiktokPost` (xem [`internal/parser/tiktok_post.go`](internal/parser/tiktok_post.go)) và POST tới backend API (`API_URL`). Payload gửi đi có dạng:

```javascript
{
  index: "not_classify_org_posts",
  upsert: true,
  data: [{
    doc_type: 1,
    crawl_source: 2,
    crawl_source_code: "tt",
    org_id: 1,
    pub_time: 1714089600,
    crawl_time: 1714090000,
    subject_id: "7123456789",        // video_id
    description: "...",
    url: "https://www.tiktok.com/@username/video/7123456789",
    comments: 100, shares: 50, reactions: 1000, favors: 200, views: 10000,
    auth_id: "123456", auth_name: "Display Name", auth_url: "https://www.tiktok.com/@username",
    crawl_bot: "tiktok-1",
    // ... xem đầy đủ struct TiktokPost trong internal/parser/tiktok_post.go
  }]
}
```

> `internal/repository/video_repo.go` vẫn định nghĩa interface `VideoStore` để upsert video thẳng vào Mongo, nhưng `cmd/main.go` hiện khởi tạo crawler với `videoRepo = nil` — nghĩa là đường đi thực tế là push qua API, không ghi Mongo trực tiếp. Nếu cần bật lại ghi Mongo, cần wiring lại ở `main.go`.

## Tính năng

- ✅ Crawl trực tiếp theo URL cho sẵn (video hoặc profile), không cần search
- ✅ Multi-org + multi-profile (crawl song song)
- ✅ Lấy dữ liệu qua network interception (API nội bộ của TikTok), không scrape DOM
- ✅ Mô phỏng hành vi người dùng (scroll, di chuột, xem video ngẫu nhiên)
- ✅ Dedup theo video ID, lọc video cũ hơn 7 ngày
- ✅ Đẩy kết quả bất đồng bộ theo batch sang backend API (không ghi Mongo trực tiếp — xem ghi chú trên)
- ✅ Batch processing với random sleep giữa URL/session
- ✅ Scheduler lặp 30-45 phút, tự nghỉ khung giờ đêm (00:00-03:00)
- ✅ Structured logging (Zap) có rotation
- ✅ Prometheus metrics (`/metrics`)
- ✅ Retry/backoff + circuit breaker khi kết nối GPM/CDP lỗi

## Anti-ban

Code đã có:

- Random sleep giữa URL (`SLEEP_MIN_URL`–`SLEEP_MAX_URL`, mặc định 60-180s)
- Random rest giữa session (`REST_MIN_SESSION`–`REST_MAX_SESSION`, mặc định 180-300s)
- Human scroll simulation, random mouse movement, random video viewing (trang profile)
- Phát hiện rate-limit/captcha (mã 2061, 10000, -1) → tạm dừng 5 phút
- Giới hạn video/URL và số trang/session (`MAX_VIDEOS_PER_URL`, `MAX_PAGES_PER_SESSION`)
- Tự nghỉ crawl trong khung giờ đêm 00:00-03:00

Khuyến nghị thêm:

- Giới hạn số lượng URL hợp lý mỗi ngày
- Dùng profile Chrome thật (có lịch sử) qua GPM
- Rotate IP nếu crawl nhiều

## Troubleshooting

### Lỗi: "please install the driver (v1.57.0) first"

→ Chạy lại:

```bash
go run github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 install chromium
```

### Lỗi: "No connection could be made... 127.0.0.1:19995"

→ GPM chưa chạy. Mở GPM Login trước.

### Lỗi: "failed to start profile"

→ Kiểm tra `PROFILE_IDS` đúng chưa. Không dùng giá trị placeholder mẫu.

### Lỗi: "No URLs found for org_ids"

→ Kiểm tra MongoDB có URL với `org_id` đó chưa:

```javascript
db.tiktok_url.find({org_id: {$in: [1,2,3]}})
```

### Lỗi: "Failed to connect GPM"

- Kiểm tra GPM đang chạy
- Kiểm tra `GPM_API` đúng (mặc định port 50325)
- Thử restart GPM

### Lỗi: "No browser context found from GPM"

- Profile chưa được start
- Thử start profile thủ công trong GPM trước
- Kiểm tra `PROFILE_IDS` đúng

### Lỗi: "Failed to connect CDP"

- GPM profile đã đóng
- Port bị block bởi firewall
- Thử restart GPM và chạy lại

### URL bị bỏ qua ("unsupported URL")

→ URL không khớp mẫu video (`/video/`) hoặc profile (`/@username`). Kiểm tra lại URL trong `tiktok_url`, phải là link TikTok hợp lệ dạng `https://www.tiktok.com/@user` hoặc `https://www.tiktok.com/@user/video/<id>`.

### Video không được đẩy sang backend

→ Kiểm tra `API_URL` đã cấu hình chưa (bỏ trống = tắt push, video sẽ không được gửi đi đâu cả). Kiểm tra log lỗi push API và endpoint backend có phản hồi status < 400 không.

## Deploy lên server khác

1. Copy toàn bộ project
2. Cài Playwright driver (xem mục Cài đặt)
3. Sửa `.env` với config đúng
4. Chạy `go run cmd/main.go`

**Hoặc build binary trên máy dev rồi copy:**

```bash
# Máy dev
build.bat

# Copy bot.exe + .env sang server
# Chạy trên server
bot.exe
```

**Lưu ý:** Playwright driver phải cài trên từng máy riêng!

## License

MIT
