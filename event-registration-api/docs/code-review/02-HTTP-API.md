# 02. HTTP 路由與請求生命週期

[← 回到架構總覽](00-架構總覽.md)

HTTP 邊界集中在 [`internal/httpapi/server.go`](../../internal/httpapi/server.go)。它使用標準庫 `net/http`，負責將 URL、header 和 JSON 轉成 core 的輸入，並將 core/store 結果轉成 HTTP status 與 JSON。

## 路由地圖

| 方法與路徑 | 認證 | 用途 |
|---|---|---|
| `GET /healthz` | 否 | 程序存活檢查 |
| `GET /readyz` | 否 | 資料庫就緒檢查 |
| `POST /v1/auth/register` | 否 | 建立一般會員 |
| `POST /v1/auth/login` | 否 | 驗證帳密並發出 bearer token |
| `POST /v1/auth/logout` | 是 | 撤銷目前 session |
| `GET /v1/me` | 是 | 讀取目前使用者 |
| `GET /v1/me/registrations` | 是 | 讀取自己的報名紀錄 |
| `GET /v1/events`, `GET /v1/events/{id}` | 否 | 查詢活動 |
| `POST /v1/events` | admin | 建立活動 |
| `PATCH /v1/events/{id}` | admin | 更新標題或描述 |
| `POST /v1/events/{id}/close` | admin | 關閉活動 |
| `POST /v1/events/{id}/registrations` | 是 | 報名活動 |
| `DELETE /v1/events/{id}/registrations/me` | 是 | 取消自己的報名 |
| `GET /v1/events/{id}/registrations` | admin | 查看活動報名紀錄 |

路由使用 Go 標準庫的 method-aware `ServeMux` pattern。活動列表和報名列表共用 `limit`、`offset`，service 限制每頁最多 100 筆、offset 不超過 10000。

## Middleware 與 handler

`routes` 的包裝順序由內而外是：

```text
mux → recoverPanic → logging → requestID
```

所以請求依序經過 request ID、請求記錄、panic recovery，再進入路由。每個 handler 只處理自己的 HTTP endpoint；需要使用者身分時直接呼叫 `currentUser`，由它讀取 `Authorization: Bearer <token>` 並請 `core.Service.Authenticate` 驗證。

這個專案沒有抽出通用的 authentication middleware。好處是路由和授權呼叫位置直接可見；review 時則要逐一檢查每個私有 handler 是否確實執行 `currentUser`，以及需 admin 的操作最後是否也在 core 檢查角色。

## JSON 與錯誤邊界

`decodeOrReply` 僅接受 `application/json`，限制 body 為 1 MiB，拒絕未知欄位和第二個 JSON value。這降低拼錯欄位卻靜默忽略的可能。每個 DTO 是 HTTP input shape，例如 `createEventRequest`，再明確轉換成 `core.CreateEventInput`，不直接把 HTTP request struct 傳到資料庫層。

`serviceError` 以 `errors.Is` 將 core 錯誤映射為 400、401、403、404、409 或 500；對外錯誤使用固定訊息，避免回傳 SQL 細節。錯誤 JSON 格式是 `{ "code": "...", "message": "..." }`。

## Review 時追問

- 每條需要登入的路由是否都驗證 token？
- 每條需要管理員的路由是否同時有 HTTP 層身份驗證和 core 層角色授權？
- login response 有設定 `Cache-Control: no-store`；其他回應是否可能包含需避免快取的敏感資料？
- `statusWriter` 是否會限制未來需要 `Flusher`、`Hijacker` 或 `Pusher` 的 handler？目前端點都是一般 JSON request/response。
- 新增或修改路由時，是否同步更新 [`openapi.yaml`](../../openapi.yaml) 和 [測試／API 契約 review](06-測試與API契約.md)？
