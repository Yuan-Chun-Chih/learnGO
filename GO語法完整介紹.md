# Go 語法與工程語意

## 摘要

Go 以小型語言核心、靜態型別、顯式錯誤與組合優先為設計方向。其價值不在提供最多語法特性，而在讓大型服務的依賴、併發與部署行為保持可讀。這份文件聚焦語言語意與後端工程中最常遇到的邊界。

## 1. Module、package 與可見性

Go module 由 go.mod 定義。package 以資料夾為單位；名稱首字母大寫代表可被其他 package 匯出。公開名稱形成相容性 contract，因此不應為了方便而擴大 export surface。

```go
package user

type User struct {
    ID    int64
    Email string
}

func New(email string) User { return User{Email: email} }

func normalize(email string) string { return strings.ToLower(email) }
```

- import graph 不允許循環；循環通常表示 package 邊界或資料流責任不清。
- internal 目錄限制 module 外部 import，適合保護業務與基礎設施實作。
- cmd 目錄放可執行程式入口；main 主要負責設定、依賴組裝、signal 與 lifecycle。
- go.sum 是依賴完整性資料，不應手動編輯以排除問題。

## 2. 型別、零值與自訂型別

### 2.1 基本型別

常用型別包含 bool、string、int、int64、float64、byte 與 rune。int 的寬度取決於架構；跨 API、資料庫與序列化邊界通常應明確使用 int64 或其他固定寬度型別。

float64 不應處理金額。金額可使用最小貨幣單位的整數，或在需要高精度十進位時使用 decimal library。

### 2.2 零值

每個 Go 型別都有可用的零值：數字為 0、bool 為 false、string 為空字串、pointer/map/slice/channel/function/interface 為 nil。零值可降低初始化負擔，但 API 必須區分「未提供」「空值」「零值」時，通常需要 pointer 或明確 option type。

```go
type PatchUser struct {
    Name *string `json:"name"`
}
```

nil slice 可以 append，讀取 nil map 也安全；對 nil map 寫入會 panic。JSON 序列化時，nil slice 通常輸出 null，空 slice 輸出 []，這是 API contract 的一部分。

### 2.3 Defined type 與 alias

```go
type UserID int64       // 新型別，避免與其他 int64 混用
type LegacyID = int64   // alias，等同於 int64
```

domain ID、貨幣、狀態碼等適合使用 defined type。這能讓編譯器阻止不合理的混用，但不應為每個暫時值建立型別。

## 3. 控制流程與函式

Go 僅有 for 迴圈；switch 預設不會 fallthrough。if 可以包含短宣告，常用於將 error scope 限制在判斷內。

```go
if err := store.Save(ctx, user); err != nil {
    return fmt.Errorf("save user: %w", err)
}
```

短宣告需注意 shadowing。若外層已有 err，內層使用 := 可能建立新的變數，使外層流程沒有收到預期錯誤。

函式支援多回傳值，慣例為 (value, error)。named return 適合非常小且語意明確的函式；大型函式使用它會使返回值來源難以追蹤。

defer 在所在函式返回時以 LIFO 順序執行，適合 Close、Unlock、Rollback 與 span.End。defer 不應在未知次數的大迴圈內累積資源。

## 4. Collection 與資料所有權

### 4.1 Array 與 slice

array 的長度是型別的一部分，傳遞時會複製；slice 是底層 array 的檢視，包含 pointer、length、capacity。subslice 與 append 可能共享底層資料。

```go
base := []int{1, 2, 3, 4}
part := base[:2]
part = append(part, 99) // 可能改寫 base[2]
```

跨 layer 或跨 goroutine 保存 slice 時，若呼叫端仍可修改原資料，應建立 defensive copy：

```go
owned := append([]string(nil), input...)
```

### 4.2 Map

map 適合 key-value 查找；不存在的 key 回傳元素型別的零值，因此需要 comma-ok 區分缺失與有效零值。

```go
value, ok := scores[key]
```

map 迭代順序未定義。map 也不是並行安全；多 goroutine 讀寫必須使用 mutex、channel 或其他所有權設計。

## 5. Struct、method 與 interface

struct 是主要資料模型。API request/response、database row、domain entity 與 cache value 通常應是不同 struct，即使欄位看似相同；這避免一層的變更意外改變另一層的 contract。

值 receiver 適合小型不可變值或不修改 receiver 的 method；pointer receiver 適合修改資料、避免大型複製，或維持一致 method set。

```go
func (a *Account) Deposit(amount int64) error {
    if amount <= 0 {
        return errors.New("amount must be positive")
    }
    a.Balance += amount
    return nil
}
```

interface 是隱式實作。實務上，介面應由使用方依真正需要的行為定義，並保持小型。

```go
type UserReader interface {
    FindByID(ctx context.Context, id UserID) (User, error)
}
```

這種設計讓 service 依賴能力，而不是特定資料庫實作。不要預先為每個 struct 建立 interface，也不要將空泛的 CRUD interface 當成架構。

embedding 提供 composition 與 promoted method；它不是傳統繼承。embedding 後的公開 method 仍是 API surface，名稱衝突與隱式行為需要審慎管理。

## 6. Error、panic 與 recover

Go 以 error 表示預期失敗。error 應提供操作脈絡，並以 %w 保留原因鏈。

```go
var ErrNotFound = errors.New("not found")

func GetUser(ctx context.Context, id UserID) (User, error) {
    user, err := repo.FindByID(ctx, id)
    if err != nil {
        return User{}, fmt.Errorf("find user %d: %w", id, err)
    }
    return user, nil
}
```

errors.Is 用於 sentinel error；errors.As 用於擷取具有資料的 typed error。錯誤字串不是穩定 contract。

panic 適合程式不變條件遭破壞或啟動時無法恢復的狀態，不適合 validation、not found 或外部服務失敗。HTTP server 的 recovery middleware 可避免單一請求終止整個 process，但應記錄 stack 並回傳安全錯誤。

## 7. Context 與取消語意

context 代表 request-scoped deadline、取消與 metadata。它必須由入口往下傳遞到 HTTP、RPC、DB 與 queue 操作。

```go
ctx, cancel := context.WithTimeout(parent, 2*time.Second)
defer cancel()
```

限制：

- 不傳 nil context；使用 context.Background 或由 request 取得。
- 不將 context 存在 struct。
- 不使用 context.Value 傳遞一般函式參數。
- timeout 不等於 retry；retry 必須有冪等性與總時間預算。

## 8. Goroutine、channel 與同步

goroutine 是輕量執行單位，但仍消耗記憶體、排程與外部連線資源。每個 goroutine 都需要 owner、停止條件、錯誤處理與等待策略。

channel 適合工作傳遞、事件通知與所有權轉交；mutex 適合保護共享可變狀態。選擇標準是資料流與所有權，不是語法偏好。

```go
select {
case result := <-results:
    return result, nil
case <-ctx.Done():
    return Result{}, ctx.Err()
}
```

channel 關閉由唯一 sender 負責；receiver 不應 close channel。buffer 是容量與背壓決策，不是消除同步問題的萬用開關。

sync.Mutex 保護短臨界區。持鎖期間避免網路 I/O、DB query、callback 或未知時間操作。WaitGroup 等待一組工作完成；errgroup 可整合 error 與 context cancellation。atomic 適合簡單數值或狀態，不適合取代複雜資料結構的同步。

go test -race 能找出部分 data race，但無法證明所有並行邏輯正確。

## 9. Generic、reflection 與 unsafe

generic 適合重複的資料結構與演算法，例如 map/filter、set、repository-adjacent utility。domain 邏輯多半使用具體型別更清楚。

reflection 適合 JSON、ORM、framework 與通用工具；業務規則使用 reflection 通常表示型別模型不足。

unsafe 破壞 Go 的記憶體安全保證，除非有可量測且不可替代的需求，不應進入一般服務程式。

## 10. I/O、JSON 與 HTTP

io.Reader 與 io.Writer 是 Go 串流抽象的核心。它們允許檔案、HTTP body、壓縮器與 buffer 組合，而不必一次載入全部資料。每個外部資源都需要明確關閉。

JSON request 應使用專用 DTO，並限制 body 大小、處理 decode error、決定未知欄位策略。公開 API 的時間建議使用 UTC 與 RFC 3339；資料庫使用帶時區欄位。

HTTP client 應重用、設定 timeout，並在讀取後關閉 response body。HTTP server 需設定 read、write、idle timeout，並在收到 termination signal 時進行 graceful shutdown。

## 11. Testing、品質與效能

| 工具 | 目的 |
|---|---|
| testing / httptest | 單元測試與 handler 測試 |
| integration test | 驗證 PostgreSQL、Redis、transaction、migration |
| go test -race | data race 偵測 |
| benchmark | 比較明確工作負載下的成本 |
| pprof / trace | CPU、記憶體、blocking、scheduler 分析 |
| gofmt / go vet | 格式與常見靜態問題 |
| fuzzing | parser、decoder、輸入邊界的健壯性 |

最佳化應從量測開始。allocation、escape、GC 與 pool 的調整都有正確性和可讀性成本；未量測的微優化通常不是瓶頸。

## 12. 與 TypeScript 的語意差異

| 主題 | Go | TypeScript / Node.js |
|---|---|---|
| 錯誤 | 顯式 error 回傳 | exception / rejected promise 常見 |
| 並行 | goroutine、channel、sync | event loop、Promise、worker thread |
| 型別 | 編譯成原生 binary 的靜態型別 | 靜態檢查在編譯期，runtime 為 JavaScript |
| 部署 | 單一 binary 常見 | Node runtime、package 與 build artifact |
| abstraction | struct、interface、composition | class、union、decorator、functional pattern 都常見 |

比較的核心不是哪一種語言「更好」，而是服務需求與團隊上下文。Go 較適合強調併發、資源效率、可預測部署與長期維護的服務；TypeScript 對前端、BFF、SDK 整合與全端迭代通常更有優勢。

## 參考

- [Effective Go](https://go.dev/doc/effective_go)
- [Go blog: context](https://go.dev/blog/context)
- [Go standard library](https://pkg.go.dev/std)
- [Go testing](https://pkg.go.dev/testing)
