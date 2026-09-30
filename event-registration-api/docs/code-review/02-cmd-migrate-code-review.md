# Code Review：`cmd/migrate`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

主要檔案：[`cmd/migrate/main.go`](../../cmd/migrate/main.go)。這個一次性命令連線 PostgreSQL，載入編譯進程式的 Goose migration，並將 schema 升級到最新版本。

## 執行流程

1. 讀取必要的 `DATABASE_URL`。
2. 以 pgx `database/sql` driver 開啟連線池，使用 30 秒 context ping DB。
3. 呼叫 `goose.SetBaseFS(migrations.Files)`，讓 Goose 從 embedded filesystem 讀 SQL，而不是依賴容器工作目錄中的 migration 檔案。
4. 設定 PostgreSQL dialect，執行 `goose.UpContext(ctx, db, ".")`。
5. 成功後記錄 migration 完成；任何錯誤以非零結束狀態停止程序。

Compose 把這個命令當作一次性 container，API 依賴它成功結束後才啟動。Migration 內容由 [`internal/migrations`](07-migrations-code-review.md) 管理。

## Review 重點

- 啟動 ping 與整體 migration 共用 30 秒 context；大型資料搬移時可能需要調整 timeout 或拆成可觀察的部署工作。
- 使用 `log.Fatal` 會直接呼叫 `os.Exit`，defer 不會執行；目前程式在 fatal 分支前連線 pool 通常不含需手動釋放的單一 socket，但若希望一致關閉，可改成 `run() error` 再由 `main` 結束。
- `SetBaseFS`、`SetDialect` 是 Goose package 層級設定；目前這是單獨執行檔與測試流程，若未來同一 process 多次呼叫或平行跑不同 dialect，需避免共享全域狀態造成干擾。
- Migration 是否安全，取決於 SQL 本身的向前相容、鎖定時間和資料回復策略；Goose 成功執行不等於 migration 對 production 無風險。

## Review 問題

- migration 超過 30 秒時，部署會如何處理？
- 新版 API 能否與 migration 後 schema 並存，支援逐步 rolling deployment？
- migration 失敗後是修正並重跑、向前補 migration，還是回復資料？Down SQL 可能刪除資料，不能假設可以無損 rollback。
