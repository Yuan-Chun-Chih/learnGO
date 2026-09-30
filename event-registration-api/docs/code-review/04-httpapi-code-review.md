# Code Review：`internal/httpapi`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

主要檔案：[`internal/httpapi/server.go`](../../internal/httpapi/server.go)。此 package 是 HTTP adapter，將方法、路徑、header 和 JSON body 轉成 `core.Service` 呼叫，再將 domain error 轉為 HTTP response。

## 路由與權限檢查位置

`routes` 使用 method-aware `http.ServeMux`，集中註冊 health、auth、me、events 和 registrations 路由。需要登入的 handler 會呼叫 `currentUser` 解析 Bearer token；admin 操作再把 actor 交給 core，由 core 再次檢查 role。

重要路由：

| 路由 | 實作 handler | 權限 |
|---|---|---|
| `POST /v1/auth/register` | `registerUser` | 公開 |
| `POST /v1/auth/login` | `login` | 公開 |
| `GET /v1/me` | `me` | 登入 |
| `POST /v1/events` | `createEvent` | admin |
| `GET /v1/events/{id}` | `getEvent` | 公開 |
| `POST /v1/events/{id}/registrations` | `registerForEvent` | 登入 |
| `DELETE /v1/events/{id}/registrations/me` | `cancelRegistration` | 登入 |
| `GET /v1/events/{id}/registrations` | `listEventRegistrations` | admin |

## 請求與回應邊界

每個輸入 body 使用獨立 DTO，`decodeOrReply` 限制 1 MiB、要求 JSON、拒絕未知欄位和多餘 JSON value。handler 將 DTO 欄位明確轉成 core input，而不是讓 SQL layer 理解 HTTP struct。

`serviceError` 以 `errors.Is` 映射 domain error 到 400、401、403、404、409、408 或通用 500。錯誤 response 固定 `{code, message}` 格式；未處理錯誤記錄在 server log，不把 SQL 細節回給 client。

Middleware 包裝後的實際請求順序：

```text
requestID → logging → recoverPanic → ServeMux → handler
```

## Review 重點

- 私有 handler 是否都呼叫 `currentUser`？新增路由時要逐條確認。
- `Cache-Control: no-store` 目前設在 login response；任何新增 token 或個人資料 response 都要重新評估快取政策。
- `statusWriter` 只包裝基本 `ResponseWriter` 方法；若新增 streaming、WebSocket 或需要 `Flusher`/`Hijacker` 的功能，要保留底層 optional interfaces。
- `decodeOrReply` 的錯誤訊息不區分 JSON 語法錯誤、body 太大或欄位型別不合；若 API client 需要更精確錯誤，可在不洩漏內部細節下細分。
- `openapi.yaml` 是手動維護；路由和 DTO 變更要同步更新規格，並加入契約驗證。

## 往下追

業務規則見 [`internal/core`](05-core-code-review.md)；SQL、交易和錯誤映射見 [`internal/store`](06-store-code-review.md)。API schema 對照見 [OpenAPI review](10-openapi-code-review.md)。
