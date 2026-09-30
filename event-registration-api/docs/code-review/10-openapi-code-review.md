# Code Review：OpenAPI 規格

[← 回到架構總覽](00-架構總覽.md)

## 範圍

規格位於 [`openapi.yaml`](../../openapi.yaml)，描述 12 個 path、HTTP methods、request/response schema、錯誤狀態、分頁參數和 Bearer authentication。它是 API consumer 和 server implementation 之間的契約，也是新增 endpoint 時的 review 清單。

## 和程式碼的對照點

- paths/methods ↔ [`httpapi.Server.routes`](../../internal/httpapi/server.go)
- request schema ↔ `registerUserRequest`、`loginRequest`、`createEventRequest`、`updateEventRequest`
- bearer security ↔ `currentUser` 與 `core.Service.Authenticate`
- domain error/status ↔ `serviceError`
- event/registration/user response schema ↔ `internal/core` JSON model tags

## Review 重點

- OpenAPI 目前手動維護，不會由 Go DTO 生成，也沒有 CI drift check；新增或改路由時需一起修改規格。
- `409` 代表多種衝突，`Error.code` 用以區分 `ALREADY_REGISTERED`、`EVENT_FULL`、`EVENT_CLOSED` 和 `CONFLICT`。應確認每種實際錯誤碼都在規格/使用說明中出現。
- 檢查 optional、nullable、default 和 required 是否符合 `encoding/json` 行為。特別是 pointer 欄位、`omitempty` 和時間格式。
- 檢查每條 endpoint 的安全需求、成功 status、空 body（204）和錯誤 response 是否都和 handler 一致。
- 若 client code generation 變重要，可在 CI 加 OpenAPI lint、文件驗證和 generated client compile；目前規格文件沒有自動化驗證。
