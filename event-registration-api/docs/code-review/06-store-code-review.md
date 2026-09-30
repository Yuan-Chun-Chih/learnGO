# Code Review：`internal/store`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

主要檔案：[`internal/store/postgres.go`](../../internal/store/postgres.go)。`Postgres` 實作 `core.Store`，也提供 readiness 使用的 `Ping`。SQL query 都用 `database/sql` 與 context 執行。

## 查詢與錯誤轉換

單列查詢使用 `QueryRowContext` 並集中 scan；列表查詢關閉 `Rows` 並檢查 `rows.Err()`。UUID 參數使用 `::uuid`，回應 ID 用 `id::text` 掃描成 core 的 string。

`translate` 將 `sql.ErrNoRows`、常見 PostgreSQL SQLSTATE（UUID 格式、unique、foreign key、check/not-null）轉成 core error。未分類錯誤向上傳遞，由 HTTP adapter 回 500。

## 報名與取消的 transaction

`Register` 在 transaction 中先條件式扣除活動名額，再 insert registration，成功後 commit。若 insert 發生有效報名 unique violation，回 `ErrAlreadyRegistered`，defer rollback 會還原先前的扣額。當更新不到 row 時，查詢活動判斷 not found、closed/started 或 full。

`CancelRegistration` 在同一 transaction 中標記有效報名取消，再將活動 `remaining` 加一；若更新名額未成功，回錯並 rollback。

## Review 時要注意的併發行為

- 鎖順序可能相反：`Register` 先更新活動 row 再 insert registration；`CancelRegistration` 先更新 registration row 再更新活動 row。相同 user/event 的取消與重新報名若重疊，可能形成 deadlock。SQLSTATE `40P01` 目前未轉成 retryable error，會落到通用 500。可統一鎖定順序或加入有限 retry。
- 活動已滿時，duplicate request 會先被 capacity 條件擋下，回 `ErrFull`；只有有名額時才會由 partial unique index 回 `ErrAlreadyRegistered`。兩者都是 409，但 `code` 可能不同。
- DB 的 `remaining` check 只保證範圍，不會自動等於有效報名數；一致性仰賴交易流程。整合測試有直接查詢兩者驗證。
- 報名扣額的 SQL 使用 DB `now()`，未命中時的 fallback 使用 Go `time.Now()`；若需嚴格的時間判定一致性，可統一時間來源。
- `UpdateEvent` 透過 `COALESCE` 保留未提供欄位；若更新不到 row，額外查詢區分 not found 和已開始／關閉。

## Review 問題

- transaction 中的每一個錯誤出口是否都回滾？
- SQLSTATE 是否有完整且合理的 public error mapping？
- 新增 query 的排序欄位是否有對應索引？
- 使用 `LIMIT/OFFSET` 時，排序是否包含穩定 tie-breaker？活動列表與報名列表目前都包含 ID。
