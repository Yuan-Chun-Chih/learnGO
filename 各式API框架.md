# Go API 框架與服務介面

## 摘要

API framework 的選擇不應從效能排行開始，而應先決定 API 型式、呼叫端、契約、身份模型、部署邊界與可觀測性。Go 的 net/http 已足以建立完整 HTTP API；第三方框架主要改善路由、middleware、binding、驗證整合或 contract workflow。

## 1. API 類型地圖

| 類型 | 通訊模型 | Go 選項 | 主要用途 |
|---|---|---|---|
| HTTP / REST | request-response、JSON | net/http、chi、Gin、Echo、Fiber | 公開 API、App、後台 |
| Contract-first HTTP | OpenAPI schema | Huma、oapi-codegen、ogen | 多團隊與第三方整合 |
| RPC | Protobuf / typed message | gRPC、Connect | 內部服務、跨語言、streaming |
| GraphQL | query graph | gqlgen | 前端資料聚合 |
| SSE | server-to-client stream | net/http | 通知、狀態與進度 |
| WebSocket | full-duplex connection | websocket libraries | 聊天、協作、即時互動 |
| Async API | queue / event | Asynq、Watermill、Kafka client | 長工作與事件整合 |
| Gateway / BFF | 聚合與邊界控制 | Envoy、Kong、Go service | 多服務對外介面 |

## 2. net/http：共同基線

net/http 的 Handler 介面簡單且通用：

```go
type Handler interface {
    ServeHTTP(http.ResponseWriter, *http.Request)
}
```

這使 middleware、router、observability 與測試工具可以共享介面。Go 1.22 的 ServeMux 已支援 method 與 path pattern，適合小型服務或希望減少依賴的專案。

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /health", healthHandler)
mux.HandleFunc("GET /users/{id}", getUserHandler)
```

正式 server 應設定 ReadHeaderTimeout、ReadTimeout、WriteTimeout、IdleTimeout 與 graceful shutdown；預設值不應直接視為 production policy。

## 3. Router 與 REST framework

### 3.1 chi

chi 建立於 net/http 之上，middleware 與 handler 都維持標準介面。優點是相依邊界清楚、測試簡單、第三方相容性高。適合作為長期 REST API 的預設候選。

### 3.2 Gin

Gin 提供 Context、route group、JSON binding、recovery 與大量範例。它適合快速建立常規商業 API。業務層不應接受 gin.Context；handler 應萃取 context.Context、DTO 與 identity，再呼叫 use case。

### 3.3 Echo

Echo 的功能定位與 Gin 接近，提供 router、middleware、binding 與 rendering。若團隊已有 Echo 經驗，沒有必要為了風格差異遷移；新專案可依既有 tooling 與 API boundary 選擇。

### 3.4 Fiber

Fiber 建立在 fasthttp 上，API 接近 Express。它在某些工作負載有吸引力，但與 net/http 的相容性不是完全等價。使用前必須確認 middleware、observability、reverse proxy、library 與團隊除錯經驗都能配合。

| 需求 | 建議 |
|---|---|
| 標準庫、低耦合、可組合 | net/http 或 chi |
| 快速 REST CRUD、團隊範例多 | Gin 或 Echo |
| 已驗證 fasthttp 生態與 Express 熟悉度 | Fiber |
| API contract 優先 | Huma / OpenAPI codegen |

## 4. HTTP middleware 與請求生命週期

典型順序：

```text
recover → request ID → real IP / trusted proxy
→ structured log → metric / trace → CORS
→ authentication → authorization → validation → handler
```

middleware 應有單一責任。它可能短路回應，但不應在 response 已寫入後繼續執行下游流程。特別注意：

- real IP 只能信任受控 proxy 的 forwarding header。
- CORS 是瀏覽器策略，不是 API authorization。
- recovery 讓 process 存活，但不會修復資料一致性問題。
- authentication 驗證身分；authorization 驗證該身分是否能對特定資源執行動作。

## 5. REST contract

### 5.1 Resource 設計

```text
GET    /v1/users?limit=20&cursor=...
POST   /v1/users
GET    /v1/users/{id}
PATCH  /v1/users/{id}
DELETE /v1/users/{id}
```

路徑以資源名詞為主。動作型操作可使用子資源或明確 command endpoint，但需說明其 idempotency 與狀態轉換，例如 POST /v1/orders/{id}/cancel。

### 5.2 Status 與錯誤

| 類別 | 狀態碼範例 | 語意 |
|---|---|---|
| 輸入格式錯誤 | 400 | JSON、query、path 不符合語法 |
| 業務驗證失敗 | 422 | 格式合法但不符合業務規則 |
| 未認證 / 未授權 | 401 / 403 | 身分缺失或權限不足 |
| 資源不存在 | 404 | 資源不可見或不存在 |
| 衝突 | 409 | unique、版本或狀態衝突 |
| 依賴失敗 | 502 / 503 / 504 | 上游、暫時不可用或 timeout |

穩定錯誤碼應與 HTTP status 分開。內部 SQL、stack trace、secret、host 位址不能出現在公開 response。

### 5.3 Pagination 與 idempotency

大型資料集優先使用 cursor / keyset pagination。offset 適合小型後台列表，但在大 offset 下成本與一致性較差。

建立訂單、付款、webhook 等可能重送的操作應支援 idempotency key，並將 key、request hash、結果與有效期納入資料模型。重試不是無條件安全的行為。

## 6. Contract-first HTTP 與 OpenAPI

OpenAPI 是 API schema、文件、client generation、測試與治理的共同語言。contract-first 適合公開 API、前後端並行與跨團隊整合；代價是 schema 演進、code generation 與向後相容需要被納入 CI。

- Huma 以 Go type 定義輸入輸出並產生 OpenAPI。
- oapi-codegen 與 ogen 可從 OpenAPI schema 產生 Go server/client。
- Schema 變更應區分 additive、deprecated、breaking change。
- 範例、錯誤碼、auth scheme、rate limit 與 pagination 都屬於 contract。

## 7. Authentication 與安全

Browser-first 應優先考慮 secure、httpOnly、sameSite cookie 與 CSRF 防護。Mobile 或第三方 API 常用 OAuth 2.0 / OIDC，或短效 access token 搭配 refresh token。

JWT 是 token 格式，不是完整認證策略。驗證至少包含簽名演算法、issuer、audience、expiration、not-before 與 key rotation。密碼以 Argon2id 或 bcrypt 雜湊；登入、重設密碼與 token endpoint 必須採 rate limit 與審計。

輸入層需限制 body size、content type、multipart upload、decode 深度與 timeout。檔案應存入 object storage，不直接信任檔名或 MIME type。

## 8. gRPC 與 Connect

gRPC 使用 Protobuf 定義 service 與 message，適合內部服務、跨語言 contract 和 streaming。

```proto
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
```

關鍵不是 RPC call 本身，而是 schema versioning、deadline、status code、retry policy、interceptor、health check、reflection 與 load balancing。streaming 需要額外處理 flow control、cancel、resource limit 與 client disconnect。

Connect 保留 Protobuf contract，同時降低與 HTTP/1.1、browser、proxy 整合的摩擦。選擇 gRPC 或 Connect 應依現有平台、client 語言與 gateway 需求決定。

## 9. GraphQL、即時與事件 API

GraphQL 適合前端需要跨資源組合資料且畫面需求變動快的情況。gqlgen 可由 schema 產生 resolver。主要風險是 N+1 query、過深 query、field-level authorization、cache 與 query cost；dataloader、complexity limit 與 persisted query 是常見控制手段。

SSE 比 WebSocket 簡單，適合單向 server push。WebSocket 適合雙向協作，但需設計 authentication、room、presence、heartbeat、backpressure 與 reconnect。

非同步 API 不應只被視為「背景執行」。queue 與 event 需要明確 delivery guarantee、retry、dead letter、ordering、idempotency、schema version 與監控。資料庫 outbox 可降低「交易成功但事件未送出」的雙寫風險。

## 10. 可觀測性、測試與交付

每個 request 至少應具有 request ID、結構化 log、HTTP metric 與 trace context。常用 API 指標包括 request count、error rate、p50/p95/p99 latency、in-flight request、rate-limit rejection、DB pool wait、upstream timeout。

測試分層：

- httptest 驗證 handler、middleware、header、status 與 JSON contract。
- integration test 驗證 DB、Redis、transaction、migration 與外部 adapter。
- contract test 驗證 OpenAPI、Protobuf 或 consumer expectation。
- load test 評估端到端延遲與容量，不只測 router 吞吐量。
- security test 覆蓋 authorization、input size、token、upload 與 replay。

## 參考

- [Go net/http](https://pkg.go.dev/net/http)
- [chi](https://github.com/go-chi/chi)
- [Gin](https://gin-gonic.com/docs/)
- [gRPC Go](https://grpc.io/docs/languages/go/)
- [OpenAPI Specification](https://spec.openapis.org/oas/latest.html)
- [OWASP API Security](https://owasp.org/www-project-api-security/)
