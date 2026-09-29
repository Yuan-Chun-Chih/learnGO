# 活動報名 API

一個可直接用 Docker Compose 啟動的 Go 後端練習成品：會員註冊與登入、管理員建立活動、活動報名與取消、名額控制、PostgreSQL migration、OpenAPI 規格及整合測試。刻意只使用 Go 標準庫的 `net/http` 作為 HTTP 層，讓路由、middleware、JSON、狀態碼與錯誤處理都能直接看懂；資料庫存取使用 `database/sql` + pgx driver，migration 使用 Goose。

## 快速啟動

先安裝 Docker Desktop，確認 Docker Engine 正在執行。在本目錄的 PowerShell 執行：

```powershell
docker compose up --build -d
docker compose ps
Invoke-RestMethod http://localhost:18080/readyz
```

Compose 會依序啟動 PostgreSQL、執行 migration，再啟動 API。主機上的 API 預設位於 `http://localhost:18080`（可由 `.env` 的 `API_PORT` 修改），PostgreSQL 開發連線埠是 `localhost:5433`。資料庫密碼預設是**只供本機練習**的 `localpass`；要更改請複製 `.env.example` 為 `.env` 並修改 `POSTGRES_PASSWORD`。如果已經建立過資料卷，更改密碼不會重設既有 PostgreSQL 帳戶密碼。

建立第一個管理員（這是明確執行的一次性操作，不會在每次啟動時重設密碼）：

```powershell
$env:ADMIN_EMAIL = 'admin@example.com'
$env:ADMIN_NAME = 'Demo Admin'
$env:ADMIN_PASSWORD = 'change-this-demo-password'
docker compose run --rm -e ADMIN_EMAIL -e ADMIN_NAME -e ADMIN_PASSWORD admin
Remove-Item Env:ADMIN_PASSWORD
```

`admin` 命令是 upsert：若 email 已存在，會把該帳號設為管理員並重設密碼。請只在可信任的本機環境使用；正式部署應另行設計受控的管理員建立流程。

## 試跑完整流程

下面的命令可直接貼入 PowerShell。`startsAt` 設為明天，因為 API 不允許建立已開始的活動。

```powershell
$base = 'http://localhost:18080'
$adminLogin = Invoke-RestMethod -Method Post -Uri "$base/v1/auth/login" -ContentType 'application/json' -Body (@{ email = 'admin@example.com'; password = 'change-this-demo-password' } | ConvertTo-Json)
$adminHeaders = @{ Authorization = "Bearer $($adminLogin.token)" }
$startsAt = (Get-Date).ToUniversalTime().AddDays(1).ToString('yyyy-MM-ddTHH:mm:ssZ')
$event = Invoke-RestMethod -Method Post -Uri "$base/v1/events" -Headers $adminHeaders -ContentType 'application/json' -Body (@{ title = 'Go 實作交流'; description = '一起討論 Go、SQL 和 API'; startsAt = $startsAt; capacity = 2 } | ConvertTo-Json)
$event

$memberEmail = "member-$(Get-Date -Format yyyyMMddHHmmss)@example.com"
$memberPassword = 'a-long-demo-password'
Invoke-RestMethod -Method Post -Uri "$base/v1/auth/register" -ContentType 'application/json' -Body (@{ name = 'Demo Member'; email = $memberEmail; password = $memberPassword } | ConvertTo-Json)
$memberLogin = Invoke-RestMethod -Method Post -Uri "$base/v1/auth/login" -ContentType 'application/json' -Body (@{ email = $memberEmail; password = $memberPassword } | ConvertTo-Json)
$memberHeaders = @{ Authorization = "Bearer $($memberLogin.token)" }
Invoke-RestMethod -Method Post -Uri "$base/v1/events/$($event.id)/registrations" -Headers $memberHeaders
Invoke-RestMethod -Uri "$base/v1/me/registrations" -Headers $memberHeaders
Invoke-RestMethod -Uri "$base/v1/events/$($event.id)/registrations" -Headers $adminHeaders
Invoke-RestMethod -Method Delete -Uri "$base/v1/events/$($event.id)/registrations/me" -Headers $memberHeaders
```

完整路由、輸入與回應 schema 見 [openapi.yaml](openapi.yaml)。常見錯誤是 JSON `{ "code": "...", "message": "..." }`：`401` 未登入／token 失效、`403` 權限不足、`404` 資源或有效報名不存在、`409` 活動滿額／已關閉／重複報名。JSON 請求需要 `Content-Type: application/json`，不接受未知欄位。

## 測試與停機

```powershell
go test ./...
go vet ./...
docker compose run --rm --build test
docker compose down
```

前兩個命令不需要資料庫；`test` profile 使用**獨立的暫存 PostgreSQL** 執行整合測試，不碰開發資料庫。整合測試覆蓋管理員權限、註冊登入、報名／取消、重複報名、滿額和兩個人同時搶最後一個名額。`docker compose down` 會保留開發資料卷；只有你明確要刪除本專案的資料時才使用 `docker compose down -v`。

## 架構與設計取捨

```text
cmd/api         HTTP 服務入口、資料庫連線池、優雅關閉
cmd/migrate     啟動前執行資料庫 schema migration
cmd/admin       一次性建立／重設管理員
internal/httpapi  路由、HTTP DTO、驗證、錯誤映射、middleware
internal/core     使用案例、業務規則、Store 介面
internal/store    PostgreSQL 實作、SQL 交易與錯誤轉譯
internal/migrations  版本化 SQL migration
```

依賴方向是 `HTTP → core.Store 介面 ← PostgreSQL 實作`。`core` 不知道 HTTP 或 pgx；因此業務規則可獨立測試，資料庫實作也可替換。這是教學用的小型分層架構，沒有額外的「repository + usecase + service」重複抽象。

報名使用一筆交易：先以條件式 `UPDATE events ... remaining > 0` 原子扣名額，再 `INSERT registrations`。資料庫部分唯一索引限制同一使用者對同活動只有一筆**有效**報名；若插入因重複失敗，整筆交易回滾，扣掉的名額也還原。取消則在同一交易中標記 `cancelled_at` 並加回名額。測試會查資料庫確認 `remaining` 與有效報名數一致。

密碼使用 bcrypt 雜湊，不儲存原文；session token 由密碼學隨機數產生，只把 SHA-256 摘要存入資料庫。這個範例沒有信箱驗證、密碼重設、速率限制、稽核記錄、可觀測性追蹤或 TLS 終止代理，**不應直接當作公開網路上的正式產品部署**。如果要上線，至少補齊這些安全與營運能力，並改用秘密管理服務提供資料庫密碼。
