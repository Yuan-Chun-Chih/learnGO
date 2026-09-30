# Code Review：`cmd/api`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

主要檔案：[`cmd/api/main.go`](../../cmd/api/main.go)。這是常駐 HTTP API 程序的 composition root：解析環境設定、建立共用依賴、啟動 server 並處理關閉訊號。

## 呼叫流程

```text
main
  → run(logger)
      → DATABASE_URL / PORT 驗證
      → sql.Open("pgx") 與連線池設定
      → PingContext
      → store.NewPostgres
      → core.NewService
      → httpapi.NewServer
      → ListenAndServe
      → signal 後 Shutdown
```

`sql.DB` 是連線池，不是單一資料庫連線。啟動時使用 5 秒 context 做 `PingContext`，而 server 對 header、讀取、寫入和 idle connection 設 timeout。SIGINT/SIGTERM 到達後，最多使用 10 秒 graceful shutdown，讓進行中的請求有機會完成。

## Review 重點

- `DATABASE_URL` 缺少或 DB 無法連線時，程序在接收流量前退出。
- 連線池上限 10、idle 上限 5，pool lifetime 30 分鐘；這些數字要和資料庫 connection limit、服務副本數一起考量。
- `PORT` 有格式和範圍檢查，但 `DATABASE_URL` 只有非空檢查；連線錯誤由 ping 揭露。
- `serverErrors` channel 緩衝一筆，避免 listener goroutine 在主 goroutine 收到 signal 時卡住送錯誤。
- `run` 可注入 logger，但 OS signal 和實際 listener 讓目前的測試較難隔離；如果要單元測試生命週期，可抽出 server builder 或 shutdown coordinator。
- HTTP server 監聽 `:PORT`，容器內接受所有 interface；主機端的 loopback 限制由 Compose port mapping 負責。

## 往下追

API handler 與 middleware 位於 [`internal/httpapi`](04-httpapi-code-review.md)，業務物件和 Store 介面位於 [`internal/core`](05-core-code-review.md)，PostgreSQL adapter 位於 [`internal/store`](06-store-code-review.md)。
