# Code Review：`internal/core`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

主要檔案：[`internal/core/core.go`](../../internal/core/core.go)。此 package 定義 API 業務使用的模型、錯誤、`Store` interface 和 `Service`。它不依賴 HTTP request、URL 或具體 PostgreSQL adapter。

## 主要元件

- Models：`User`、`Account`、`Event`、`Registration` 與兩種報名列表 view。
- Domain errors：`ErrInvalid`、`ErrUnauthorized`、`ErrForbidden`、`ErrFull`、`ErrClosed` 等，讓 adapter 可用 `errors.Is` 判斷。
- `Store` interface：宣告註冊、session、活動和報名需要的持久化操作。
- `Service`：驗證輸入、檢查 actor role、處理密碼和 token，再呼叫 Store。

依賴方向是 `Service → Store interface ← store.Postgres`。interface 定義在消費它的 core package，避免 core 反向依賴 PostgreSQL。

## 身份驗證與權限

`RegisterUser` trim 名稱、將 email 轉小寫、檢查長度和格式、bcrypt 雜湊密碼。`Login` 比對 hash，成功後用 `crypto/rand` 產生 32 bytes token，只將 SHA-256 token hash 和到期時間傳到 Store。`Authenticate` 和 `Logout` 使用相同 hash 函式定位 session。

建立、更新、關閉活動，以及查看活動報名名單都檢查 `actor.Role == "admin"`。會員自己的報名則以 actor user ID 限定查詢和取消範圍。

## 業務邊界

core 可驗證輸入範圍，例如活動容量、標題和開始時間，但不能先讀 `remaining` 再決定是否報名。名額會被多個請求同時競爭，因此正確性由 [`store.Postgres.Register`](../../internal/store/postgres.go) 的資料庫 transaction 保證。

## Review 重點

- `Account` 包含 `PasswordHash`，`User` 不包含；對外 response 應繼續使用不含 hash 的模型。
- 登入不存在帳號與密碼錯誤都回 unauthorized，但只有帳號存在時才執行 bcrypt，仍可能有時間差。
- email lower-case 正規化存在於 Go 路徑；直接 SQL 寫入不會自動正規化。
- `NewService` 設定 `now: time.Now`，測試難以固定時間。若時間邊界測試增加，可注入 clock。
- 取消流程目前沒有開始時間或活動狀態限制；這是目前 policy，review 時需和產品規則核對。
- validation error 文字與限制屬於 API 可觀察行為，修改時要同步測試和 OpenAPI。
