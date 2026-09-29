# Go 後端學習資料

這個 repository 收錄 Go 語言、後端 API 與資料庫相關的技術筆記，以及一個可用 Docker Compose 啟動的 Go 活動報名 API 範例。

## 技術筆記

- [Go 框架發展與應用場景](go框架發展與應用場景.md)
- [Go 語法完整介紹](GO語法完整介紹.md)
- [Go API 框架](各式API框架.md)
- [資料庫框架與存取工具](資料庫框架與存取工具%20%20.md)
- [Nakama Backend 筆記](NakamaBackend.md)

## 實作範例

[活動報名 API](event-registration-api/README.md) 使用 Go `net/http`、PostgreSQL、Goose migrations 與 Docker Compose，涵蓋使用者登入、管理員活動管理、名額控制、報名和取消。API 規格位於 [OpenAPI 文件](event-registration-api/openapi.yaml)。

在 `event-registration-api` 目錄執行：

```powershell
docker compose up --build -d
Invoke-RestMethod http://localhost:18080/readyz
```

詳細啟動、試用和測試方式請看範例專案 README。
