# Code Review：測試程式碼

[← 回到架構總覽](00-架構總覽.md)

## 測試檔案

- [`internal/core/core_test.go`](../../internal/core/core_test.go)：不依賴資料庫的輸入驗證、角色權限、分頁和 token hash 單元測試。
- [`internal/httpapi/server_integration_test.go`](../../internal/httpapi/server_integration_test.go)：使用 `httptest.Server`、實際 HTTP handlers 和 PostgreSQL 的整合測試；由 `//go:build integration` build tag 控制。

## 測試範圍

整合測試自行套用 embed migration，建立 admin/member，透過 HTTP 註冊、登入、建活動、報名、取消、查看名單、登出。它也直接查 DB 確認取消釋出名額，以及兩個使用者同搶最後名額只有一個成功。

`compose.yaml` 的 `test` profile 提供獨立 `db-test`，資料目錄是 tmpfs，避免整合測試接觸開發用 `event_lab`。

## 執行方式

一般單元測試：

```powershell
go test ./...
```

在 Docker 中跑整合測試：

```powershell
docker compose run --rm --build test
```

Integration tag 測試要求 `TEST_DATABASE_URL`。不要手動將它指向有價值或 production 資料庫，因測試會執行 migration 並新增測試資料。

## Review 重點

- 整合測試目前依賴獨立暫存 DB，測試本身沒有 truncate fixture；如果改用長期測試 DB，需考慮資料隔離與清理。
- 併發測試的 goroutine 呼叫共用 HTTP test helper；若 request 失敗，應確保錯誤可回傳到測試主 goroutine，避免只在 goroutine 中 `Fatal` 造成診斷困難。
- 一般單元測試尚未完整覆蓋有效登入、錯誤密碼、session expiry、活動更新、非法 JSON/Content-Type/body limit 和錯誤映射。
- 最後名額測試驗證一種競爭情境，不能取代 deadlock、取消與重報名交錯、資料庫重啟等競爭測試。
- 測試時間使用 `time.Now()`；若測試開始時間邊界，注入 clock 會更穩定。
