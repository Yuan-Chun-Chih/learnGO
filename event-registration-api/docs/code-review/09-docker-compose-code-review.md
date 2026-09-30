# Code Review：Dockerfile 與 Docker Compose

[← 回到架構總覽](00-架構總覽.md)

## 範圍

- [`Dockerfile`](../../Dockerfile)：編譯與 runtime image。
- [`compose.yaml`](../../compose.yaml)：本機 PostgreSQL、migration、API、管理員工具和測試服務。
- [`.dockerignore`](../../.dockerignore)：限制 Docker build context。
- [`.env.example`](../../.env.example)：本機設定範例。

## 建置與服務拓樸

Dockerfile 的 build stage 使用 Go image 編譯三支 binary；runtime stage 使用 Alpine，只複製 binary、CA 憑證並以 UID 10001 的 `app` 使用者執行。

Compose 開發服務的啟動依賴是：

```text
db healthy → migrate 成功結束 → api 啟動
```

`db` 使用命名 volume 保存本機資料。API 主機 port 預設 `127.0.0.1:18080`，PostgreSQL 開發 port 是 `127.0.0.1:5433`。`admin` 在 `tools` profile，`db-test` 和 `test` 在 `test` profile；整合測試 DB 使用 tmpfs。

## Review 重點

- `POSTGRES_PASSWORD` 的預設值 `localpass` 只能用於本機；production 應從安全 secret source 注入，並避免寫入 image layer 或 repo。
- image 使用 `golang:1.26-alpine` 和 `alpine:3.23` tag，建置可隨 tag 更新；要可重現供應鏈時需 pin digest、管理更新和掃描漏洞。
- API port mapping 綁 loopback，但如果改成公開 host address，必須另外配置 TLS、firewall 和認證邊界。
- Compose healthcheck 只回報健康狀態，不等於自動修復或備份。migration service 的成功狀態由 API dependency gate 使用。
- `docker compose down` 會保留 named volume；`down -v` 會移除資料，需確認操作範圍。
- test database 和開發 database 是不同 service；保持這個隔離，避免測試誤寫開發資料。
