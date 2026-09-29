# Go 框架發展與應用場景

## 摘要

Go 後端生態的核心特徵不是「選一套全能框架」，而是以標準庫建立穩定底座，再按需求組合 router、資料存取、觀測、背景工作與部署工具。這種組合式風格降低框架鎖定，也要求團隊更清楚地維護邊界與工程規範。

對多數新服務，推薦的預設組合是：

```text
Go + net/http / chi + PostgreSQL + sqlc
+ migration + Redis（有明確需求時）
+ OpenAPI + structured logging + metrics + tracing
```

這不是唯一正解；它的優點是 HTTP、SQL 與交易邊界都保持可見，適合長期維護的 API 服務。

## 1. 生態類型地圖

| 類型 | 解決的問題 | 代表選項 | 選型重點 |
|---|---|---|---|
| HTTP 標準庫 | server、client、TLS、request/response | net/http | 所有 Web 服務的基線 |
| Router | 路由、path parameter、middleware 組合 | chi、httprouter | 是否維持 net/http 相容 |
| REST framework | binding、rendering、recovery、便利 API | Gin、Echo、Fiber | 團隊熟悉度與相依邊界 |
| Contract-first HTTP | OpenAPI contract、server/client generation | Huma、oapi-codegen、ogen | 對外 API 與跨團隊協作 |
| RPC | 強型別、跨語言、streaming | gRPC、Connect | 內部服務與協議需求 |
| GraphQL | client 驅動欄位選取與資料圖譜 | gqlgen | 前端聚合查詢需求 |
| SQL 存取 | driver、pool、query、transaction | database/sql、pgx | SQL 控制與 driver 功能 |
| SQL code generation | SQL 轉型別化 Go API | sqlc | SQL-first 團隊的預設候選 |
| ORM | CRUD、關聯、model 操作 | GORM、Bun | 開發速度與 SQL 可見性 |
| Schema-first ORM | schema 產生 query 與 relation API | Ent | 關聯模型與 codegen workflow |
| Migration | schema 版本與部署演進 | Atlas、Goose、golang-migrate | 可回放、可審查、零停機策略 |
| Cache / worker | 快取、rate limit、背景工作 | go-redis、Asynq | 失效策略與可靠交付 |
| Observability | log、metric、trace、profile | slog、Prometheus、OpenTelemetry | 生產環境可診斷性 |

## 2. 發展脈絡

### 2.1 標準庫優先

net/http、context、database/sql、testing、encoding/json 與 io 讓 Go 在沒有大型框架的情況下就能建立完整服務。這形成兩個工程慣例：

- HTTP handler 與 middleware 使用標準介面，第三方元件可以互換。
- 業務邏輯避免依賴 framework-specific context 或 model，以降低耦合。

標準庫不是功能不足，而是刻意將路由、驗證、ORM、OpenAPI 等政策選擇留給專案。

### 2.2 輕量 Web framework

chi 採用標準 net/http 模型，適合重視組合性、測試與低耦合的服務。Gin 與 Echo 提供較多 request binding、middleware 與 convenience API，適合一般 REST 開發。Fiber 以 fasthttp 為基礎，API 風格接近 Express，但不完全相容 net/http 生態；只有在相容性已被驗證時才應選用。

### 2.3 契約與雲原生工具

微服務與跨團隊協作提高後，API contract、可觀測性與資料 schema 的重要性高於 router 效能。OpenAPI、Protobuf、sqlc、Ent、OpenTelemetry 與 Prometheus 代表這一階段：它們將 contract、schema 與營運訊號納入程式交付流程。

## 3. Web 與 API 場景

### 3.1 一般 REST API

典型組合：

```text
chi 或 Gin
→ request validation
→ use case / service
→ sqlc query layer
→ PostgreSQL
```

適用於帳號、訂單、後台、內容管理、行動 App API 與第三方整合。主要設計重點是 request/response contract、authorization、transaction、pagination、idempotency 與觀測性，而不是 router 本身。

### 3.2 BFF 與前端整合

BFF 將多個後端資源聚合成特定前端所需的 API。TypeScript 常適合 UI 相鄰、快速迭代的 BFF；Go 則適合聚合本身存在高併發、嚴格 latency 或大量外部整合的情況。BFF 不應承載核心交易規則，否則會變成難以重用的服務層。

### 3.3 服務間通訊

gRPC 適合內部服務、高頻呼叫、明確契約與 streaming。Protobuf schema 是版本管理的核心；deadline、status code、interceptor 與向後相容規則必須與服務一起設計。Connect 適合希望使用 Protobuf，但也重視 HTTP 相容性與瀏覽器整合的場景。

REST 與 gRPC 可以並存：公開 API 使用 HTTP/JSON，內部依需要使用 RPC；不需要以「全面遷移」作為目標。

### 3.4 即時與非同步工作

- SSE 適合單向通知、狀態更新、progress stream。
- WebSocket 適合雙向互動，例如聊天與協作；需要處理 session、backpressure、斷線重連與 fan-out。
- Queue / worker 適合寄信、影像轉檔、報表、webhook retry 等長工作。訊息至少一次投遞要求 consumer 具備 idempotency。
- Event-driven 整合通常需要 outbox pattern，避免資料庫已提交但事件遺失。

## 4. 資料存取場景

### 4.1 PostgreSQL 為預設核心資料庫

具備 transaction、constraint、join、索引與成熟工具鏈的關聯式資料庫，應是帳務、訂單、會員、權限等核心資料的預設選擇。PostgreSQL 的 JSONB、全文搜尋與 pgvector 也能延後引入專用系統的時點。

### 4.2 SQL-first：database/sql、pgx、sqlc

database/sql 提供通用 pool 與 transaction 抽象；pgx 提供 PostgreSQL driver 與進階功能。sqlc 讓 SQL 維持在版本控制中，並產生型別化 query 方法。此路線的價值在於 query plan、join、lock 與 transaction 都可直接審查。

### 4.3 ORM：GORM 與 Ent

GORM 適合常規 CRUD、後台與需要快速建立 model 操作的服務；複雜 query 仍應檢視 SQL 與 N+1 問題。Ent 適合 schema-first、關聯較複雜、願意採用 code generation 的團隊。兩者都不能取代 migration、索引設計、transaction 與資料庫觀測。

## 5. 架構選擇

### 5.1 預設：模組化單體

大多數服務應從模組化單體開始。以 feature 為單位分組，讓 handler、use case、repository 與 domain model 在同一業務模組內可追蹤：

```text
cmd/api
internal/
  user/
  order/
  billing/
  platform/
```

模組化單體保留本地 transaction、簡單部署與低營運成本；它不是「不具架構」，而是將邊界建立在程式內。

### 5.2 Clean / Hexagonal Architecture

這兩種架構的共同目的，是讓業務規則不依賴 HTTP、ORM、queue 或 framework。實務上可保留簡單的 handler → service/use case → repository 分層，並將 ports 僅用在真正需要可替換的外部依賴。為每個 struct 建 interface 或建立過多 adapter，通常只會增加間接層。

### 5.3 微服務的門檻

服務拆分需要明確理由：獨立部署節奏、不同擴展模型、強隔離需求、明確資料所有權或組織邊界。若沒有這些理由，微服務會增加網路失敗、分散式 transaction、觀測、版本與 on-call 複雜度。

## 6. Go 相對 TypeScript 的位置

| 面向 | Go 較適合 | TypeScript 較適合 |
|---|---|---|
| 執行模型 | 高併發 I/O、worker、長時間服務 | 前端與 UI 相鄰服務 |
| 部署 | 單一 binary、低 runtime 依賴 | npm 整合與快速產品迭代 |
| 型別與 runtime | 編譯產物直接執行、介面簡潔 | 豐富型別表達與 JavaScript 生態 |
| 平台工作 | CLI、infra、network service、資料處理 | BFF、SaaS 整合、全端產品 |
| 團隊成本 | 長期一致性與資源成本 | 全端同語言與 SDK 可用性 |

語言選擇應由服務責任、延遲與吞吐需求、部署環境、既有 SDK、團隊能力與維運成本共同決定。Go 與 TypeScript 在同一系統內分工是常見且合理的架構。

## 7. 決策原則

1. 先選資料模型與一致性，再選 ORM 或資料庫。
2. 先用 net/http 理解邊界，再選 router 或 REST framework。
3. 先建立模組化單體，再以明確成本效益拆服務。
4. 對公開 API 優先投資 contract、版本、錯誤語意與 idempotency。
5. 對生產服務優先投資 timeout、log、metric、trace、migration 與 integration test。
6. 不以 benchmark 單獨決定框架；端到端延遲常由資料庫與外部依賴主導。

## 參考

- [Go standard library](https://pkg.go.dev/std)
- [Go net/http](https://pkg.go.dev/net/http)
- [Go database/sql](https://pkg.go.dev/database/sql)
- [sqlc documentation](https://docs.sqlc.dev/)
- [GORM documentation](https://gorm.io/docs/)
- [Ent documentation](https://entgo.io/docs/getting-started/)
