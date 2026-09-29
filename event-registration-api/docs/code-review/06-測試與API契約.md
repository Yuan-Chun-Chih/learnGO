# 06. 測試與 API 契約

[← 回到架構總覽](00-架構總覽.md)

測試分為不需外部服務的 core 單元測試，以及連接真實 PostgreSQL 的 HTTP 整合測試。端點規格寫在 [`openapi.yaml`](../../openapi.yaml)，操作說明寫在 [`README.md`](../../README.md)。

## 單元測試

[`internal/core/core_test.go`](../../internal/core/core_test.go) 目前涵蓋：註冊輸入驗證、活動建立的角色與容量檢查、分頁範圍、token hash 基本性質。測試可直接建 `Service`，在預期 validation/authorization error 的分支不需呼叫 Store。

這些測試速度快，但不是所有業務流程的完整規格：例如有效密碼建立帳號、登入過期、更新活動以及取消權限的邊界，仍主要由整合測試或尚未覆蓋。

## PostgreSQL 整合測試

[`internal/httpapi/server_integration_test.go`](../../internal/httpapi/server_integration_test.go) 以 `httptest.Server` 經過真正的 HTTP handler 和 PostgreSQL adapter。它要求 `TEST_DATABASE_URL`，並以同一份 embed migration 準備 schema。`//go:build integration` 表示一般 `go test ./...` 不會執行這些測試。

現有整合案例檢查：

- 管理員建立活動、會員不能建立活動。
- 會員註冊、登入、建立 session，再使用 token 呼叫 endpoint。
- 報名、重複報名／滿額衝突、取消後釋出名額、管理員查看名單、登出後 token 失效。
- 兩個會員同時搶最後一個名額時只有一個成功，並直接查 DB 驗證剩餘名額和有效報名數。

Compose 將測試連到獨立的 `db-test` service，資料放 tmpfs，和開發用 `db` 分開。不要把 `TEST_DATABASE_URL` 指到有價值的資料庫；測試會套 migration 並建立使用者、活動、session 和報名紀錄。

## OpenAPI 對照

`openapi.yaml` 定義路徑、輸入輸出 schema、Bearer security scheme、分頁 query 和錯誤狀態。它目前是手動維護，沒有程式碼生成或 CI schema drift 驗證。因此每次改 handler、DTO、錯誤 code 或授權規則，都要 review 規格是否同步。

建議從一個 endpoint 逐項比對：HTTP method/path → 是否需要 security → request body 欄位 → 成功 status/body → 可能的 error code/status → 整合測試是否涵蓋。特別留意 `409` 可能是 `ALREADY_REGISTERED`、`EVENT_FULL`、`EVENT_CLOSED` 或一般 `CONFLICT`。

## Review 時追問

- 測試是否涵蓋錯誤路徑、併發和資料庫約束，而不只是 happy path？
- 測試 helper 的 token、email 和開始時間是否每次執行都唯一且穩定？
- 每個對外 response 是否能在 OpenAPI schema 找到？
- 是否要為 OpenAPI 加入 lint/schema validation，避免文件和 handler 漂移？
- 是否需增加 handler 層測試以覆蓋非法 JSON、未知欄位、錯誤 Content-Type、超過 body limit、未授權與錯誤映射？
