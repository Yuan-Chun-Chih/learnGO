# 04. PostgreSQL 存取與交易

[← 回到架構總覽](00-架構總覽.md)

[`internal/store/postgres.go`](../../internal/store/postgres.go) 是 `database/sql` 到 PostgreSQL 的 adapter。它負責 SQL、row scanning、交易、rows affected 判斷，以及將 PostgreSQL／driver 錯誤轉為 core 可理解的錯誤。

## 一般存取模式

Store 使用帶 context 的 `QueryRowContext`、`QueryContext` 和 `ExecContext`。UUID 在查詢參數位置明確轉型為 `::uuid`，回傳值以 `id::text` 掃描到 core 的 string ID。列表 query 先 `defer rows.Close()`，逐筆 scan，再回傳 `rows.Err()`。

`translate` 會將 `sql.ErrNoRows` 轉成 `ErrNotFound`，UUID 格式錯誤轉成 `ErrInvalid`，unique violation 轉成 `ErrConflict`，外鍵錯誤轉成 `ErrNotFound`。其他 PostgreSQL 錯誤原樣往上傳，最終由 HTTP 層轉成通用 500。

## 報名交易與容量競爭

`Register` 的核心順序：

1. `BeginTx`。
2. 對指定活動執行條件式更新：`status = 'open'`、`starts_at > now()`、`remaining > 0` 才扣一個名額。
3. 若 `RowsAffected` 為零，再讀活動狀態，回報 not found、closed/started 或 full。
4. 新增 registration，最後 commit。

條件式 `UPDATE` 會在資料庫端競爭同一活動 row lock，因此兩位使用者搶最後名額時，最多一筆扣減成功。若新增報名觸發有效報名的 unique index，函式回 `ErrAlreadyRegistered`，defer 的 rollback 會一併還原剛才的名額扣減。

## 取消交易

`CancelRegistration` 先把使用者該活動的有效報名更新成 `cancelled_at = now()`，再將活動 `remaining` 加一，並要求新值仍小於 capacity。兩步驟在同一 transaction；中途失敗時 defer rollback，避免只取消不還名額或只加名額不取消。

## Review 發現的併發注意點

- `Register` 先鎖活動 row，再插入報名；`CancelRegistration` 先鎖報名 row，再鎖活動 row。相同使用者和活動若同時發生重新報名與取消，存在 event→registration 與 registration→event 的相反鎖順序，可能形成 PostgreSQL deadlock。`translate` 目前沒有將 SQLSTATE `40P01` 映射成可重試錯誤，HTTP 最後會回 500。可考慮統一鎖順序，或明確加入有限次 retry 和測試。
- 活動已滿時，重複報名請求會先因 `remaining > 0` 不成立而進入滿額分支，回 `ErrFull`；只有仍有名額時，unique index 才會將它辨識為 `ErrAlreadyRegistered`。HTTP 都是 409，但錯誤 code 可能不同。若契約要求穩定回 `ALREADY_REGISTERED`，需調整判斷順序並顧及併發。
- `Register` 的更新以資料庫 `now()` 判斷開始時間，但 fallback 分支使用 Go `time.Now()`。通常只在更新未命中時走該分支；若需嚴格一致，可改由 SQL 查詢回傳一個一致的資料庫時間判斷。
- 取消目前不檢查活動是否已開始或關閉，這是現有行為，應確認是否符合活動規則。

## Review 時追問

- 每個 `BeginTx` 的所有錯誤路徑是否都 rollback？目前以 defer 保護，成功路徑 commit。
- 哪些唯一性或參照完整性由資料庫負責，而非只由 Go 預先查詢？
- 新增 retry 時如何避免重送已成功但 client 未收到回應的非冪等操作？
- `UpdateEvent` 使用 `COALESCE` 保留未提供欄位；API DTO 使用 pointer 區分欄位省略與空字串，這個 PATCH 語意是否清晰？
