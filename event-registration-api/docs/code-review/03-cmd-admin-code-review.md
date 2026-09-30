# Code Review：`cmd/admin`

[← 回到架構總覽](00-架構總覽.md)

## 範圍

主要檔案：[`cmd/admin/main.go`](../../cmd/admin/main.go)。這是一次性管理員 bootstrap 命令，不是 HTTP endpoint，也不會在 API 啟動時自動執行。

## 執行流程

```text
讀取 DATABASE_URL / ADMIN_EMAIL / ADMIN_NAME / ADMIN_PASSWORD
  → trim 名稱、正規化 email
  → 驗證 email、名稱長度、密碼 byte 長度
  → bcrypt.GenerateFromPassword
  → 開啟 PostgreSQL pool
  → Store.UpsertAdmin
```

名稱未提供時使用 `Administrator`。密碼限制為 12–72 bytes，以配合 bcrypt 輸入長度限制。資料庫只收到 bcrypt hash，不會保存密碼原文。

## Upsert 的實際效果

[`store.UpsertAdmin`](../../internal/store/postgres.go) 以 email 作為衝突鍵。若帳號不存在就新增 admin；若帳號已存在，則更新名稱、密碼 hash 並把 role 改成 admin。因此重複執行不是單純「確認存在」，而是可能升權並重設密碼。

## Review 重點

- 命令只做基本 email 格式和長度檢查，資料庫 unique constraint 是最後的唯一性保護。
- 密碼從環境變數讀取，PowerShell history、CI log、部署紀錄或 process environment 都可能暴露它；部署流程應用安全的 secret injection。
- 程式用 `log.Fatal` 結束錯誤；同 migration command 一樣，fatal 會略過 defer。可考慮改用回傳 error 的 `run` 函式以便測試和一致清理。
- `UpsertAdmin` 本身沒有管理員審核或 audit log；本機練習可接受，production 需限制操作者並記錄權限變更。
- 不應把 admin 密碼寫入 Compose、Dockerfile、README 範例或提交到 `.env`。
