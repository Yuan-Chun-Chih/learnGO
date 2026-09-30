# Code Review：`internal/migrations`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

Package 包含 [`embed.go`](../../internal/migrations/embed.go) 和 [`00001_create_schema.sql`](../../internal/migrations/00001_create_schema.sql)。前者透過 `//go:embed *.sql` 將 migration 放進 binary；後者是目前唯一的 Goose schema migration，建立 users、sessions、events 和 registrations。

Migration 的執行程式在 [`cmd/migrate`](02-cmd-migrate-code-review.md)，SQL 存取實作在 [`internal/store`](06-store-code-review.md)。

## Schema 約束和索引

- `users.email` unique，role 只允許 `member`/`admin`，name/email 長度有 check constraints。
- `sessions.token_hash` 是 primary key，user 外鍵使用 `ON DELETE CASCADE`；expired session 有索引，但目前沒有清除排程。
- `events` 以 UUID 為主鍵，限制容量正值、remaining 在 0 到 capacity 間，並以 `(starts_at, id)` 支援列表順序。
- `registrations` 外鍵關聯 event/user。`registrations_one_active_idx` 是 partial unique index：只對 `cancelled_at IS NULL` 的紀錄限制 event/user 唯一。
- user/event registration lookup 各有索引。

## Up / Down 審查

Goose `Up` 依版本套用 migration，Down 段依相依反向移除 registrations、events、sessions、users。Down 會刪掉整個 schema 資料；不能視為安全的 production rollback。現有資料庫升級應新增 migration，不要直接修改已套用到別處的 `00001`。

## 設計限制

`remaining BETWEEN 0 AND capacity` 是單列 constraint，無法確保 `remaining = capacity - active registrations`。這個跨資料列條件由 store transaction 維護。若任何新流程直接 insert/delete registration 而沒有同步調整 remaining，就會造成資料漂移。

email 應用程式路徑會先轉小寫，但資料庫 unique 比較仍是大小寫敏感；如需 DB 層大小寫不敏感，可另行評估 `citext` 或 expression index，並規劃既有資料遷移。

## Review 問題

- 新 migration 是否具備舊資料回填、索引建置鎖定和部署相容性方案？
- Down 是否會永久丟資料？是否應以向前修復取代回滾？
- 刪除 user/event 的 FK 行為是否符合歷史紀錄保留政策？registrations 預設阻止刪除，sessions 則 cascade。
- 容量是否未來允許下修？目前沒有下修 capacity 的 API，將來若增加要確保不能小於已報名人數。
