這個專案最重要的理解是：

> `services/nakama` 不是 Gin、Echo 或 Fiber 的 HTTP API 專案，而是「Nakama 遊戲後端框架 + Go Runtime Plugin」。

Go 在這裡不是獨立啟動 HTTP Server，而是編譯成 `backend.so`，由 Nakama 載入，實作遊戲 RPC、配對、即時對戰、排行榜、Guild 與資料儲存邏輯。

---

## 1. 專案整體架構

```text
前端 / price-feed worker
          │
          ▼
     Nakama Server
          │
          ├── HTTP API / Socket / gRPC
          ├── Authentication
          ├── Matchmaker
          ├── Group / Guild
          ├── Leaderboard
          ├── Storage
          └── Go Runtime Plugin
                    │
                    ├── RPC handlers
                    ├── Authoritative Match
                    ├── Business rules
                    └── Storage access
```

此專案的主要目錄是：

```text
services/nakama/
├── runtime.go
├── runtime_config.go
├── runtime_security.go
├── runtime_rpc_helpers.go
├── runtime_finance_*.go
├── runtime_community_*.go
├── runtime_market*.go
├── runtime_maintenance.go
├── *_test.go
├── go.mod
├── Dockerfile
├── docker-compose.yml
└── local.yml
```

---

## 2. 這裡的「Go 框架」到底是什麼？

專案使用的核心框架是：

```go
github.com/heroiclabs/nakama-common/runtime
```

它提供 Nakama Runtime 的介面，例如：

- `runtime.NakamaModule`
- `runtime.Initializer`
- `runtime.Match`
- `runtime.Logger`
- `runtime.MatchDispatcher`
- `runtime.Presence`

因此這個專案的關係是：

```text
Go 語言
  ↓
Nakama Go Runtime SDK
  ↓
Nakama Server
  ↓
遊戲後端功能
```

它不是典型的：

```text
Go
  ↓
Gin / Echo / chi
  ↓
net/http
  ↓
API Server
```

### 和 Gin、Echo 的差異

Gin 或 Echo 的程式通常會自己建立 HTTP server：

```go
router := gin.Default()
router.POST("/orders", createOrder)
router.Run(":8080")
```

但本專案不會自己呼叫：

```go
http.ListenAndServe(...)
```

因為 HTTP、Socket、認證與路由都由 Nakama 負責。Go 程式只需要向 Nakama 註冊功能。

---

## 3. `package main` 為什麼沒有 `main()`？

所有檔案大多是：

```go
package main
```

但專案沒有一般的：

```go
func main() {}
```

原因是它不是一般執行檔，而是 Plugin。

Dockerfile 中有：

```dockerfile
RUN go build --trimpath --buildmode=plugin -o ./backend.so
```

`-buildmode=plugin` 會把 Go 程式編譯成共享函式庫：

```text
backend.so
```

Nakama 啟動時會載入這個檔案，尋找特定的匯出函式：

```go
func InitModule(...)
```

所以這裡的 `InitModule` 就相當於這個 Plugin 的入口點。

---

## 4. `go.mod` 與 Plugin 建置

專案的 `go.mod`：

```go
module nakama-finance-runtime

go 1.26.1

require github.com/heroiclabs/nakama-common v1.45.0
```

這代表：

- Module 名稱是 `nakama-finance-runtime`
- 使用 Go 1.26.1
- 依賴 Nakama Runtime API
- 由 Go Module 管理相依套件

Dockerfile 使用：

```dockerfile
FROM registry.heroiclabs.com/heroiclabs/nakama-pluginbuilder:3.38.0 AS builder
```

最後執行：

```dockerfile
go build --buildmode=plugin -o ./backend.so
```

這裡有一個重要觀念：

> Go Plugin 通常需要和宿主程式使用相容的 Go、作業系統、架構與依賴版本。

因此 Nakama 版本與 Plugin Builder 版本要對齊。這也是為什麼 Dockerfile 明確指定 Nakama `3.38.0`。

---

# 5. `InitModule`：整個 Runtime 的組合根

檔案：

[ runtime.go ](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime.go)

核心函式：

```go
func InitModule(
    ctx context.Context,
    logger runtime.Logger,
    db *sql.DB,
    nk runtime.NakamaModule,
    initializer runtime.Initializer,
) error
```

這個函式展示了 Go 常見的依賴注入概念。

## 每個參數的意義

### `ctx context.Context`

代表目前初始化操作的上下文，包含：

- 取消訊號
- deadline
- request-scoped metadata

初始化過程中的資料庫或 Nakama 操作，都可以使用這個 context。

### `logger runtime.Logger`

由 Nakama 注入的 logger：

```go
logger.Info("Nakama runtime initialized")
logger.Error("RegisterRpc failed: %v", err)
```

Runtime 不需要自行建立 logging framework。

### `db *sql.DB`

Nakama 提供的資料庫連線入口。

這個專案很多函式寫成：

```go
func rpcCreateGuildHandler(
    ctx context.Context,
    logger runtime.Logger,
    _ *sql.DB,
    nk runtime.NakamaModule,
    payload string,
) (string, error)
```

其中：

```go
_ *sql.DB
```

表示該函式目前不直接使用 SQL，而是透過：

```go
nk.StorageRead(...)
nk.StorageWrite(...)
nk.GroupCreate(...)
nk.LeaderboardWrite(...)
```

存取 Nakama 所管理的資料。

### `nk runtime.NakamaModule`

這是最重要的依賴，提供 Nakama 的服務能力：

- Group / Guild
- Storage
- Leaderboard
- Match
- User
- Notification
- Session
- Matchmaker

可以把它理解成：

```text
NakamaModule = Nakama 後端服務的抽象 API
```

### `initializer runtime.Initializer`

負責註冊 Plugin 提供的功能：

```go
initializer.RegisterRpc(...)
initializer.RegisterMatch(...)
initializer.RegisterMatchmakerMatched(...)
```

---

# 6. Runtime 啟動流程

`InitModule` 中的流程大致是：

```text
Nakama 載入 backend.so
        │
        ▼
呼叫 InitModule
        │
        ├── 建立排行榜
        ├── 初始化市場資料
        ├── 註冊 authoritative match
        ├── 註冊 matchmaker callback
        └── 註冊所有 RPC
```

程式概念如下：

```go
func InitModule(...) error {
    if err := registerLeaderboards(ctx, nk); err != nil {
        return err
    }

    if err := seedDefaultMatchPriceFeed(ctx, nk); err != nil {
        return err
    }

    if err := initializer.RegisterMatch(
        financeAuthoritativeModule,
        func(...) (runtime.Match, error) {
            return &financeAuthoritativeMatch{}, nil
        },
    ); err != nil {
        return err
    }

    if err := initializer.RegisterMatchmakerMatched(
        financeMatchmakerMatched,
    ); err != nil {
        return err
    }

    for id, fn := range rpcs {
        if err := initializer.RegisterRpc(
            id,
            withRPCRateLimit(id, fn),
        ); err != nil {
            return err
        }
    }

    return nil
}
```

這是一個典型的「啟動時註冊」架構。

---

# 7. RPC 註冊：Go 的函式型別與 Registry

專案用 map 管理 RPC：

```go
rpcs := map[string]func(
    context.Context,
    runtime.Logger,
    *sql.DB,
    runtime.NakamaModule,
    string,
) (string, error){
    rpcGetPhaseConfig:        rpcGetPhaseConfigHandler,
    rpcCreateGuild:           rpcCreateGuildHandler,
    rpcCreateFinanceMatch:    rpcCreateFinanceMatchHandler,
    rpcFinanceMatchmaking:    rpcFinanceMatchmakingHandler,
}
```

這裡有三個重要 Go 概念。

## 7.1 函式可以當成值

Go 的函式可以：

- 放進變數
- 當作參數傳遞
- 放進 map
- 從函式回傳

例如：

```go
type Handler func(string) (string, error)

handlers := map[string]Handler{
    "hello": handleHello,
}
```

本專案就是使用這種方式建立 RPC registry。

## 7.2 Handler 有統一函式簽名

在 `runtime_security.go` 中定義：

```go
type runtimeRPCHandler func(
    context.Context,
    runtime.Logger,
    *sql.DB,
    runtime.NakamaModule,
    string,
) (string, error)
```

這代表所有 RPC handler 都必須符合同一種介面。

例如：

```go
func rpcCreateGuildHandler(
    ctx context.Context,
    logger runtime.Logger,
    db *sql.DB,
    nk runtime.NakamaModule,
    payload string,
) (string, error)
```

這種統一簽名讓 Nakama 能以相同方式呼叫所有 RPC。

## 7.3 `withRPCRateLimit` 是 Decorator

註冊時不是直接傳入 handler：

```go
initializer.RegisterRpc(id, fn)
```

而是：

```go
initializer.RegisterRpc(id, withRPCRateLimit(id, fn))
```

`withRPCRateLimit` 會回傳一個新的 handler：

```go
func withRPCRateLimit(
    id string,
    handler runtimeRPCHandler,
) runtimeRPCHandler
```

它的流程是：

```text
收到 RPC
  ↓
取得 user ID 或 client IP
  ↓
檢查 rate limit
  ↓
超過限制 → 回傳 resource exhausted
  ↓
未超過 → 執行原始 handler
```

這就是典型的 Decorator Pattern。

---

# 8. RPC 的完整請求流程

以 `create_guild` 為例：

```text
前端呼叫 create_guild
        │
        ▼
Nakama 驗證 session
        │
        ▼
找到 RPC registry 中的 handler
        │
        ▼
withRPCRateLimit
        │
        ▼
rpcCreateGuildHandler
        │
        ├── 從 context 取得 user ID
        ├── 解析 JSON payload
        ├── 驗證 guild name
        ├── 呼叫 Nakama Group API
        ├── 寫入 Storage
        ├── 寫入 audit log
        └── 回傳 JSON 字串
```

Handler 的基本形式：

```go
func rpcCreateGuildHandler(
    ctx context.Context,
    logger runtime.Logger,
    _ *sql.DB,
    nk runtime.NakamaModule,
    payload string,
) (string, error) {
    userID, err := requireUserID(ctx)
    if err != nil {
        return "", err
    }

    var input createGuildPayload
    if err := parsePayload(payload, &input); err != nil {
        return "", err
    }

    if strings.TrimSpace(input.Name) == "" {
        return "", invalidArgument("Guild name is required.")
    }

    group, err := nk.GroupCreate(...)
    if err != nil {
        return "", err
    }

    return mustJSON(map[string]interface{}{
        "guildId":  group.GetId(),
        "accepted": true,
    }), nil
}
```

---

# 9. `context.Context` 在這個專案中的用途

專案透過 context 取得 Nakama 注入的使用者資訊：

```go
func requireUserID(ctx context.Context) (string, error) {
    userID := contextString(
        ctx,
        runtime.RUNTIME_CTX_USER_ID,
    )

    if userID == "" {
        return "", unauthenticated("Unauthenticated user.")
    }

    return userID, nil
}
```

使用者名稱也是從 context 取得：

```go
contextString(ctx, runtime.RUNTIME_CTX_USERNAME)
```

Client IP 則被 rate limiter 使用：

```go
runtime.RUNTIME_CTX_CLIENT_IP
```

因此這裡的 context 不只是取消控制，也承載 Nakama Runtime 提供的 request metadata。

---

# 10. `parsePayload[T any]`：本專案的 Generic

在 `runtime_rpc_helpers.go`：

```go
func parsePayload[T any](
    payload string,
    target *T,
) error {
    if strings.TrimSpace(payload) == "" {
        return invalidArgument("Payload is required.")
    }

    if err := json.Unmarshal(
        []byte(payload),
        target,
    ); err != nil {
        return invalidArgument("Payload is invalid JSON.")
    }

    return nil
}
```

呼叫方式：

```go
var input createGuildPayload

if err := parsePayload(payload, &input); err != nil {
    return "", err
}
```

這裡的：

```go
[T any]
```

表示 `parsePayload` 可以接受任何型別：

```go
parsePayload(payload, &createGuildPayload{})
parsePayload(payload, &financeHistoryPayload{})
parsePayload(payload, &matchCandleIngestPayload{})
```

`any` 等同於：

```go
interface{}
```

但 Generic 能讓函式保留型別資訊，避免每次都手動做型別斷言。

---

# 11. 錯誤處理：不是 HTTP status，而是 gRPC status code

專案有這些 helper：

```go
func invalidArgument(message string) error {
    return runtime.NewError(message, grpcInvalidArgument)
}

func notFound(message string) error {
    return runtime.NewError(message, grpcNotFound)
}

func permissionDenied(message string) error {
    return runtime.NewError(message, grpcPermissionDenied)
}
```

例如：

```go
return "", invalidArgument("guildId is required.")
```

這裡的 `grpcInvalidArgument` 是 gRPC status code，不是 HTTP `400`。

常見對應關係：

| Runtime Error | 語意 |
|---|---|
| `UNAUTHENTICATED` | 沒有登入或 token 無效 |
| `PERMISSION_DENIED` | 已登入但沒有權限 |
| `NOT_FOUND` | 找不到資料 |
| `INVALID_ARGUMENT` | 輸入格式錯誤 |
| `FAILED_PRECONDITION` | 目前狀態不允許操作 |
| `RESOURCE_EXHAUSTED` | 超過 rate limit |
| `OUT_OF_RANGE` | 數值或數量超出範圍 |

這個設計比所有錯誤都回傳一般字串更好，因為前端可以根據錯誤類型處理。

---

# 12. Authoritative Match：這個專案最重要的 Go 框架概念

檔案：

[ runtime_finance_match.go ](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime_finance_match.go)

專案實作一個金融對戰房間：

```go
type financeAuthoritativeMatch struct{}
```

這個 struct 沒有欄位，因為真正的房間狀態放在：

```go
type financeAuthoritativeMatchState struct {
    ArenaID            string
    Players            []string
    Presences          map[string]runtime.Presence
    Portfolios         map[string]*financePortfolio
    Orders             map[string]*financeOrder
    Ready              map[string]bool
    Status             string
    PriceCents         int64
    MarketStale        bool
}
```

這是 Go 中「行為物件」與「狀態物件」分開的做法：

```text
financeAuthoritativeMatch
    └── 提供 Match 方法

financeAuthoritativeMatchState
    └── 保存房間目前狀態
```

---

## 12.1 `MatchInit`

```go
func (m *financeAuthoritativeMatch) MatchInit(
    ctx context.Context,
    logger runtime.Logger,
    db *sql.DB,
    nk runtime.NakamaModule,
    params map[string]interface{},
) (interface{}, int, string)
```

回傳三個值：

```text
state        → match 的初始狀態
tick rate    → 每秒執行幾次
label        → 給 Nakama / matchmaker 的識別資訊
```

專案中：

```go
return state,
    authoritativeMatchTickRate,
    mustJSON(map[string]interface{}{
        "type":    gameTypeFinance,
        "arenaId": state.ArenaID,
    })
```

`state` 是：

```go
*financeAuthoritativeMatchState
```

但函式回傳型別是：

```go
interface{}
```

所以後續 callback 必須做型別斷言：

```go
matchState, ok := state.(*financeAuthoritativeMatchState)
if !ok || matchState == nil {
    return state
}
```

這是 Go 使用 interface 保存不同具體狀態時的常見寫法。

---

## 12.2 `MatchJoinAttempt`

此方法在玩家真正加入前執行：

```go
func (m *financeAuthoritativeMatch) MatchJoinAttempt(...)
```

它負責檢查：

- 玩家是否屬於指定 Community
- 玩家是否在允許的 expected players 清單中
- 房間是否已經滿員
- 玩家是否有資格加入

例如：

```go
if len(matchState.ExpectedPlayers) > 0 &&
    !matchState.ExpectedPlayers[presence.GetUserId()] {
    return state, false, "player was not selected for this match"
}
```

回傳：

```go
(interface{}, bool, string)
```

其中：

- 第一個值：更新後的 state
- 第二個值：是否允許加入
- 第三個值：拒絕原因

---

## 12.3 `MatchJoin`

玩家成功加入後，才會執行：

```go
func (m *financeAuthoritativeMatch) MatchJoin(...)
```

它會：

```go
matchState.Presences[presence.GetSessionId()] = presence
matchState.Players = append(matchState.Players, userID)
matchState.Portfolios[userID] = &financePortfolio{...}
```

接著：

```go
financePersistSnapshot(...)
financeBroadcastState(...)
```

也就是：

```text
更新房間狀態
  ↓
保存 snapshot
  ↓
廣播最新狀態給玩家
```

---

## 12.4 `MatchLoop`

這是 authoritative match 的核心：

```go
func (m *financeAuthoritativeMatch) MatchLoop(
    ctx context.Context,
    logger runtime.Logger,
    db *sql.DB,
    nk runtime.NakamaModule,
    dispatcher runtime.MatchDispatcher,
    tick int64,
    state interface{},
    messages []runtime.MatchData,
) interface{}
```

每次 loop 會做：

```text
tick 增加
  ↓
執行維護任務
  ↓
檢查市場價格是否過期
  ↓
處理玩家訊息
  ↓
處理配對確認
  ↓
處理 countdown
  ↓
處理交易訂單
  ↓
更新投資組合
  ↓
保存 snapshot
  ↓
廣播狀態
```

專案狀態流轉大致是：

```text
waiting
   │
   ▼
matched_confirming
   │
   ▼
countdown
   │
   ▼
running
   │
   ├── canceled
   └── finished
```

這種設計是「伺服器權威模式」：

> 玩家送的是操作意圖，真正的交易結果由伺服器的 authoritative match 決定。

例如玩家不應直接告訴伺服器：

```text
我的現金變成 10,000
```

而是送出：

```text
我要買入某數量的 BTC
```

然後由 server 根據：

- 當前價格
- 現金
- 持倉
- 手續費
- 稅
- 滑價
- 訂單狀態

計算最終結果。

---

# 13. Match 中的狀態為什麼使用指標？

```go
state := &financeAuthoritativeMatchState{
    Players:    []string{},
    Portfolios: map[string]*financePortfolio{},
}
```

使用指標的原因：

- 狀態很大，不希望每次複製整個 struct。
- callback 可以修改同一份狀態。
- map、slice、指標欄位可以直接更新。
- state 的生命週期由 match 管理。

方法中：

```go
matchState.Tick++
matchState.Status = "running"
matchState.Players = append(matchState.Players, userID)
```

都是直接修改 match state。

---

# 14. Nakama Storage 與 PostgreSQL 的關係

這個專案的資料流不是每一個功能都直接寫 SQL。

```text
Go Runtime
    │
    ├── nk.StorageRead/Write
    ├── nk.GroupCreate
    ├── nk.LeaderboardWrite
    └── nk.MatchCreate
          │
          ▼
      Nakama API
          │
          ▼
      PostgreSQL
```

Docker Compose 啟動 PostgreSQL：

```yaml
postgres:
  image: postgres:16-alpine
```

啟動 Nakama 前先執行：

```bash
/nakama/nakama migrate up
```

所以 PostgreSQL 主要儲存：

- Nakama users
- groups
- storage objects
- leaderboard records
- Nakama 內部資料

而 Go 程式多半透過 Nakama abstraction 存取資料：

```go
nk.StorageRead(...)
nk.StorageWrite(...)
nk.StorageList(...)
nk.StorageDelete(...)
```

---

## 14.1 Storage object 的概念

專案常使用：

```go
&runtime.StorageWrite{
    Collection: collectionFinanceOrders,
    Key:        orderID,
    UserID:     "",
    Value:      mustJSON(order),
    PermissionRead:  0,
    PermissionWrite: 0,
}
```

幾個欄位的意義：

- `Collection`：資料類別
- `Key`：資料識別值
- `UserID`：資料擁有者；空字串通常代表 global storage
- `Value`：JSON 字串
- `PermissionRead`：讀取權限
- `PermissionWrite`：寫入權限

這個專案的資料模型看起來像：

```text
collection: finance_orders
key:        arena-id:order-id
value:      JSON
```

---

## 14.2 使用 storage version 實作 CAS

維護任務中可以看到：

```go
version := objects[0].GetVersion()
```

接著寫入：

```go
Version: version
```

這是 optimistic concurrency control。

概念是：

```text
讀取資料與版本 v1
        │
        ▼
修改資料
        │
        ▼
只有資料仍然是 v1 才允許寫入
```

如果其他程序已經改過資料，版本不同，寫入就失敗。

這可以用來實作：

- lease
- idempotency
- 併發更新
- 防止重複消費
- maintenance lock

---

# 15. 專案中的資料一致性設計

這個專案有幾個很適合學習的後端模式。

## 15.1 Idempotency

`financeOrderIdempotencyRecord` 用來防止同一個 client order 重複處理。

```text
相同 clientOrderID
       │
       ├── 第一次：建立訂單
       └── 第二次：回傳原結果，不重複建立
```

這對交易、付款、訂單 API 非常重要。

## 15.2 Rate limit

```go
var runtimeRPCRateLimiter = struct {
    sync.Mutex
    Windows map[string]runtimeRateWindow
}{...}
```

使用 mutex 保護共享 map：

```go
runtimeRPCRateLimiter.Lock()
defer runtimeRPCRateLimiter.Unlock()
```

這是 Go 的同步控制範例。

## 15.3 HMAC 驗證

市場價格資料由外部 worker 傳入時，使用：

```go
hmac.New(sha256.New, []byte(secret))
```

驗證：

- secret
- timestamp
- nonce
- signature
- allowed symbol
- allowed source
- 價格變動範圍

這是 server-to-server authentication，不是一般使用者登入。

## 15.4 Outbox / Projection

排行榜投影資料使用：

```go
financeLeaderboardProjectionRecord
```

先將待同步資料存入 storage，再重試投影到 leaderboard。

這避免：

```text
比賽已完成
但排行榜更新失敗
```

造成資料永久遺失。

---

# 16. 專案的檔案分層

雖然所有檔案都是 `package main`，但仍用檔案名稱切出功能模組。

## 啟動與設定

- `runtime.go`：Plugin 入口與註冊
- `runtime_config.go`：RPC ID、collection、遊戲常數
- `runtime_security.go`：rate limit、環境變數、安全設定
- `runtime_rpc_helpers.go`：JSON、context、error helper

## Community / Guild

- `runtime_community_guild_lifecycle.go`
- `runtime_community_guild_members.go`
- `runtime_community_guild_governance.go`
- `runtime_community_guild_queries.go`
- `runtime_community_storage.go`
- `runtime_community_views.go`
- `runtime_community_models.go`
- `runtime_community_score.go`

## Finance Match

- `runtime_finance_match.go`
- `runtime_finance_orders.go`
- `runtime_finance_persistence.go`
- `runtime_finance_matchmaking.go`
- `runtime_finance_history.go`
- `runtime_finance_models.go`

## Market

- `runtime_market.go`
- `runtime_market_models.go`

## 維護任務

- `runtime_maintenance.go`

這種方式雖然沒有拆成多個 package，但仍具有 feature-oriented structure：

```text
community
finance
market
maintenance
```

在 Plugin 環境中，維持單一 `main` package 可以降低 Plugin 載入與註冊的複雜度。

---

# 17. 這個專案沒有傳統 Handler、Service、Repository 三層嗎？

它有部分分層概念，但不是典型 REST 專案的嚴格三層。

以 Guild 建立為例：

```text
rpcCreateGuildHandler
    │
    ├── requireUserID
    ├── parsePayload
    ├── ensureUserHasNoGuild
    ├── nk.GroupCreate
    ├── writeGuildMembership
    ├── writeGuildConfig
    └── appendGuildAudit
```

這裡：

- `rpcCreateGuildHandler` 接近 Controller / Application Handler
- `ensureUserHasNoGuild` 是業務規則
- `writeGuildMembership` 接近 Storage Repository
- `nk.GroupCreate` 是 Framework API
- `appendGuildAudit` 是資料持久化操作

如果系統繼續擴大，可以進一步抽成：

```text
RPC Handler
    ↓
Guild Service
    ↓
Guild Repository
    ↓
NakamaModule
```

目前專案選擇較扁平的結構，原因可能是：

- Runtime Plugin 本身已經提供大量服務
- 功能仍集中在單一遊戲後端
- 直接使用 `nk` 比額外包裝更簡單

---

# 18. 測試如何對應 Go 工程實務？

專案有：

```text
runtime_community_test.go
runtime_finance_test.go
runtime_market_test.go
runtime_security_test.go
```

測試主要集中在純邏輯：

- 交易計算
- 手續費與滑價
- 價格新鮮度
- market allowlist
- HMAC signature
- Guild role authorization
- matchmaking lease
- metrics merge

例如：

```go
func TestFinanceFeesAndSlippageUseBasisPoints(t *testing.T) {
    if got, _ := applyMatchSlippage(...); got <= ... {
        t.Fatal("...")
    }
}
```

這類測試有一個優點：

> 不需要啟動完整 Nakama 或 PostgreSQL，也能驗證核心商業規則。

但若要更完整，還可以補充：

- Nakama Storage integration test
- RPC handler integration test
- Match lifecycle test
- 併發 rate limiter test
- idempotency race test
- PostgreSQL restore / migration test

---

# 19. 這個專案最值得掌握的 Go 知識

從這個專案學 Go，建議優先觀察以下概念：

| Go 概念 | 專案位置 |
|---|---|
| package main | 所有 Runtime 檔案 |
| function type | `runtimeRPCHandler` |
| map registry | `runtime.go` 的 `rpcs` |
| interface | `runtime.NakamaModule`、`runtime.Match` |
| pointer receiver | `financeAuthoritativeMatch` |
| type assertion | `state.(*financeAuthoritativeMatchState)` |
| generic | `parsePayload[T any]` |
| context | 所有 RPC / Match callback |
| error wrapping / typed error | `runtime.NewError` |
| map / slice | Match state、players、orders |
| mutex | rate limiter |
| JSON tag | models |
| environment variables | market/security |
| Docker build | `Dockerfile` |
| testing | `*_test.go` |

---

# 20. 這個專案和一般 Go HTTP API 的一句話比較

一般 Go API：

```text
Go 程式自己管理 HTTP server、router、middleware
```

這個專案：

```text
Nakama 管理 server 與遊戲基礎能力，
Go Plugin 負責註冊 RPC、實作遊戲規則與 authoritative match
```

因此，閱讀這個專案時，不應先問：

> 「Gin 的 router 在哪裡？」

而應該問：

> 「Nakama 在哪裡呼叫我的 Plugin？我的 Plugin 在哪裡註冊 RPC 與 Match？每個 callback 的生命週期是什麼？」

這就是理解此專案 Go 框架的核心。

可先從以下順序閱讀：

1. [Dockerfile](C:\Users\kdsam\Documents\nakama-duel\services\nakama\Dockerfile)
2. [runtime.go](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime.go)
3. [runtime_rpc_helpers.go](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime_rpc_helpers.go)
4. [runtime_community_guild_lifecycle.go](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime_community_guild_lifecycle.go)
5. [runtime_finance_match.go](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime_finance_match.go)
6. [runtime_finance_persistence.go](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime_finance_persistence.go)
7. [runtime_security.go](C:\Users\kdsam\Documents\nakama-duel\services\nakama\runtime_security.go)
8. `*_test.go` 測試檔案