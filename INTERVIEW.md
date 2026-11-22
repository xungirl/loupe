# Loupe 面试读本（中文）

按「教学顺序」逐个文件讲：每个文件三层——**(a) 骨架**（它在系统里干什么、主要函数）、**(b) 为什么**（设计取舍，面试官最爱追问的部分）、**(c) 语法**（Go / Python / React 里值得会讲的写法）。力气花在 (a)(b)，(c) 扫一眼会讲就行。最后是约 25 道基于代码的面试问答。

> 所有数字以 `BENCHMARK.md` 和 `results/` 里的原始输出为准；这里只讲原理，不重复数字。

---

## 0. 一张图先记住

```
Claude Code 转录 ─┐            ┌─ SDK(OTLP JSON) ─┐   hook 脚本 ─┐
 (import-claude)  │            │                  │              │
                  ▼            ▼                  ▼              ▼
              ┌──────── HTTP ingest (/api/v1/spans, /v1/traces) ────────┐
              │  校验/截断 → 价格表算成本 → 有界队列(按 span 计) → 429   │
              └────────────────────────┬────────────────────────────────┘
                                       ▼ 单个 writer goroutine 攒批
                              SQLite (WAL)：spans / traces 汇总 / findings
                     ┌─────────────────┼──────────────────┬───────────────┐
                     ▼                 ▼                  ▼               ▼
              查询 API(组装树)   SSE live tail      检测器(settle 后跑)   retention
                     └──────────── React UI（瀑布图 / agent 树 / 实时 / 洞察 / 实验）
```

---

## 第一部分：Go 后端

### 1. `server/internal/model/span.go` + `target.go` —— 统一的数据形状

**(a) 骨架**：定义 `Span` 结构体（trace_id、span_id、parent、kind、时间、状态、token、工具输入输出……）和 `Normalize()`：校验必填字段、kind 白名单、时间先后、负数 token、未来时间，补默认值，超长内容截断。`ExtractTarget()` 从工具输入 JSON 里抽出「操作对象」（文件路径或命令）。

**(b) 为什么**：
- 三种来源（Claude Code 转录、Python SDK、OTLP）**先归一成一个扁平结构再入库**，后面的存储、检测器、UI 都不用关心数据从哪来——这是整个系统解耦的关键。
- 超长 payload **截断而不是拒绝**：一次巨大的工具输出不该让整条 trace 丢失；截断时按 UTF-8 字符边界切（`utf8Start`），否则会切出乱码。
- 只拒绝「存进去也没意义」的 span（没有 id、结束早于开始），并统一包成 `ErrInvalid`，HTTP 层用 `errors.Is` 映射成 400。
- `Target` 单独成列：检测器要按「同一个文件被改了几次」分组，提前抽出来就不用每次解析 JSON。

**(c) 语法**：结构体 tag（`json:"trace_id,omitempty"`）；`fmt.Errorf("%w: ...", ErrInvalid)` 包装错误、`errors.Is` 判断；方法接收者 `func (s *Span)` 用指针才能原地修改。

### 2. `server/internal/store/schema.go` —— 表结构

**(a) 骨架**：三张表。`spans`（`WITHOUT ROWID`，主键 `(trace_id, span_id)`）、`traces`（每条 trace 一行的汇总）、`findings`（检测结果）。

**(b) 为什么**：
- 主键 `(trace_id, span_id)`：① 取整条 trace = 主键前缀的一次范围扫描；② **重复 span 被主键挡掉**，`INSERT OR IGNORE` 天然幂等——客户端重试、hook 和导入器发同一个 span 都不会重复。
- `WITHOUT ROWID`：表就按主键聚簇存储，按 trace 取数据时相邻行在相邻页上。
- `traces` 汇总表：会话列表要显示 token/成本/错误数，如果每次都去 `GROUP BY` 百万行 span 会很慢，所以写入时**增量维护**。代价是写入时多一次 upsert。
- `tool_name` 上的部分索引（`WHERE tool_name != ''`）：只给工具 span 建索引，索引更小。

### 3. `server/internal/store/store.go` —— 写入与读取

**(a) 骨架**：`Open()` 开两个连接池；`InsertSpans()` 一个事务里批量插入 span，并只用**真正新插入的行**算出每条 trace 的增量（`traceDelta`），再 upsert 汇总表；`TraceSpans()` 按开始时间取出整条 trace。

**(b) 为什么**：
- **两个连接池**：SQLite WAL 模式是「一个写者 + 多个读者」。写池 `SetMaxOpenConns(1)`，读池多连接且 `query_only`。Go 侧的结构和数据库的并发模型一一对应，避免写写之间互相 `SQLITE_BUSY`。
- **幂等的汇总**：`res.RowsAffected()==0` 说明是重复 span，直接跳过，不计入 token/成本。所以重试一整批也不会把成本算两遍——测试 `TestInsertIsIdempotent` 专门验证。
- 汇总里 `start_ns = MIN(...)`、`end_ns = MAX(...)`：乱序到达（子 span 先到）也能得到正确的起止时间。
- 选 `modernc.org/sqlite`（纯 Go）而不是 `mattn/go-sqlite3`（cgo）：不需要 C 编译器，`go test -race`、交叉编译都简单；代价是慢一些（README 里写了这个取舍）。

**(c) 语法**：`tx.PrepareContext` 预编译语句在循环里复用；`defer tx.Rollback()`——提交成功后再 Rollback 是空操作，这是 Go 里事务的惯用写法；`ON CONFLICT(trace_id) DO UPDATE SET ... excluded.xxx` 是 SQLite 的 upsert。

### 4. `server/internal/store/queries.go` + `stats.go` —— 查询

**(a) 骨架**：`ListTraces`（筛选 + 排序 + 分页，返回总数）、`SearchSpans`、`ReplaceFindings`、`PendingDetection`、`DeleteTracesBefore`（retention）、`Stats`（按工具/模型/agent/天/检测器聚合 + 返工热点）。

**(b) 为什么**：
- 筛选条件用 `where []string` + `args []any` 动态拼接，**值永远走 `?` 占位符**，只有排序字段是从白名单 map 里取的字符串——防 SQL 注入。
- 「用了某个工具的 trace」用 `trace_id IN (SELECT trace_id FROM spans WHERE tool_name = ?)`，正好命中 `idx_spans_tool`。
- `ReplaceFindings` 把 `detected_ns` 设成**检测开始前看到的 `last_ingest_ns`**：如果检测期间又来了新 span，`last_ingest_ns > detected_ns`，下一轮会重新检测——不会漏。
- `DeleteTracesBefore` **分块删除**（每次 500 条 trace 一个事务）：单写者连接不会被一个超大删除长时间占住，写入不会卡顿。
- 排序时加 `trace_id` 作为第二关键字：同分时结果稳定，分页不会跳行。

### 5. `server/internal/ingest/pipeline.go` —— 有界队列 + 攒批写入 + 背压（**最重要的文件**）

**(a) 骨架**：`Submit()` 预留队列容量后把 job 放进 channel；一个 writer goroutine（`run()`）攒批：攒够 `MaxBatch` 个 span 或等满 `FlushInterval` 就写一次事务（`write()`），写完通知等待者、发给 live tail。`Close()` 停止接收并把队列里剩下的全部写完。

**(b) 为什么**：
- **队列按 span 数而不是请求数计**：一个请求可能带 1 个 span 也可能带 2000 个，按 span 计内存上界才可预测。
- **满了立刻拒绝（429 + Retry-After），而不是阻塞 handler**：阻塞会让 goroutine 和连接无限堆积，最终把内存撑爆；拒绝把压力推回给客户端，客户端退避重试（SDK 和导入器都实现了退避）。这就是背压。
- **CAS 循环预留容量**：多个 handler 并发 Submit，用 `CompareAndSwap` 保证 `queued` 永远不超过上限，不需要锁住整个队列。
- channel 缓冲区大小 = 队列容量（最坏情况每个 job 一个 span），所以**预留成功后的发送永远不会阻塞**。
- `mu sync.RWMutex` 保护 `closed`：Submit 持读锁发送，Close 持写锁 `close(queue)`——保证不会「向已关闭的 channel 发送」而 panic。测试 `TestConcurrentSubmitAndCloseLosesNothingAccepted` 故意让 Close 和并发 Submit 赛跑。
- **单 writer**：SQLite 本来就只能一个写者；多个 goroutine 抢着写只会互相等锁。攒批把每个 span 的事务开销（fsync 等）摊薄——批量 vs 单条的吞吐差距见 BENCHMARK.md。
- 写失败重试 3 次，仍失败就丢弃并计数（`Dropped`），**等待中的调用者会收到错误**；不会静默丢失。
- 语义：`202 Accepted` = 进了队列，不等于已落盘；要确认落盘用 `?wait=true`（返回 200）。

**(c) 语法**：`atomic.Int64`、`select` 同时等 channel 和 `timer.C`；`timer.Reset/Stop`；`sync.WaitGroup` 等 writer 退出；`for j := range ...` vs `j, ok := <-ch` 判断 channel 关闭。

### 6. `server/internal/otlp/otlp.go` —— OpenTelemetry GenAI 映射

**(a) 骨架**：解析 OTLP/HTTP **JSON** 的 `ExportTraceServiceRequest`，把 `gen_ai.*` 语义约定属性映射到 Span：`gen_ai.operation.name`（chat→llm，execute_tool→tool，invoke_agent→agent）、`gen_ai.request/response.model`、`gen_ai.usage.input/output_tokens`、`gen_ai.tool.name`、`gen_ai.agent.id/name`；不认识的属性保留到 `Attributes`。

**(b) 为什么**：
- 支持 OTel 标准意味着任何用 OpenTelemetry 打点的 agent 框架都能直接把数据发过来，不用专门适配 Loupe。
- OTLP/JSON 的坑：**ID 是十六进制字符串，int64 被编码成 JSON 字符串**（因为 JavaScript 数字精度只有 53 位）——所以写了 `flexInt` 同时接受 `123` 和 `"123"`。
- **部分成功**（partial success）：一个批次里坏的 span 计入 `rejected`，好的照样入库；不因为一个坏 span 拒绝整批。这和 OTLP 规范的设计一致。
- 只支持 JSON 不支持 protobuf：收到 protobuf 返回 415 并提示改成 `http/json`，诚实地写在局限性里。

**(c) 语法**：自定义 `UnmarshalJSON` 方法；用指针字段（`*string`）区分 tagged union 里「哪个字段存在」；闭包 `take(keys...)` 取值后从 map 删除，剩下的就是未知属性。

### 7. `server/internal/assemble/assemble.go` —— 乱序 / 迟到 span 的树组装

**(a) 骨架**：`Build(spans)`：建 `span_id → Node` 的 map；有父且父存在 → 挂到父下；没有父 → 根；有父但父不在 → **孤儿根**（`Orphan=true`）。然后 DFS 得到深度、展平顺序（瀑布图顺序）和每个 span 的「有效 agent」；最后聚合出 agent 树（谁派生了谁、各自的 token/成本/错误）。

**(b) 为什么**：
- **查询时组装，而不是写入时组装**：写入只管存，查询时拿到「当前已到达的所有 span」再建树。所以乱序完全不影响结果；兄弟节点按 `(开始时间, span_id)` 排序保证确定性——测试把同一条 trace 打乱 200 次，结果必须完全一样。
- **迟到的父节点**：父还没到时子节点作为孤儿显示（UI 标「parent not received」）而不是丢弃；父到了以后下一次查询自动挂上。
- **环**（脏数据 A→B→A）：从根出发走不到的节点就在环上，把最早的那个提升为孤儿根并断开边，保证遍历一定终止。
- agent 归属规则：agent 类型的 span 开启一个新 agent；否则用 span 自带的 agent_id；否则继承父节点的 agent。Task 工具 span 属于**调用它的** agent，子 agent 从它下面开始——这样父子 agent 的成本不会混在一起。

**(c) 语法**：`sort.Slice` 自定义比较；递归 DFS 带 `visited map`；`switch parent, ok := nodes[p]; { case ...: }` 这种 switch 前置语句。

### 8. `server/internal/api/*.go` —— HTTP 层

**(a) 骨架**：`server.go` 用 Go 1.22+ 的路由模式（`"GET /api/v1/traces/{id}"`）注册所有路由，外面包 CORS 和请求指标中间件；`ingest.go` 两个入口（原生 JSON / OTLP），都走 `submit()`：算成本 → `Pipeline.Submit` → 把错误映射成 202/200/429/503；`query.go` 是查询和 SSE。

**(b) 为什么**：
- `http.MaxBytesReader` 限制请求体大小（413），gzip 解压后再用 `LimitReader` 限一次——防「gzip 炸弹」（很小的压缩包解出巨大数据）。
- 路由分类（ingest/query/live/ops）作为 metrics 标签，而不是用原始 URL：原始 URL 里有 trace id，会造成**标签基数爆炸**。
- SSE（`handleLive`）：每个客户端订阅 broker；收到一个 span 后把缓冲里已有的一并写出，再 `Flush` 一次，减少系统调用；每 15 秒发一行注释心跳，防止代理断开空闲连接。
- `statusRecorder` 实现了 `Flush()`：中间件包了一层 ResponseWriter，如果不转发 Flush，SSE 就会被缓冲住发不出去——这是个容易踩的坑。

**(c) 语法**：`r.PathValue("id")`；带标签的 `break drain` 跳出 for+select（在 select 里直接 `break` 只会跳出 select）；`http.Flusher` 类型断言。

### 9. `server/internal/live/broker.go` —— SSE 扇出

**(a) 骨架**：每个订阅者一个带缓冲的 channel；`Publish` 遍历订阅者，用 `select { case s.C <- sp: default: 丢弃并计数 }` 非阻塞发送。

**(b) 为什么**：Publish 由 writer goroutine 调用，**绝不能被一个慢浏览器拖住**——否则整个 ingest 停摆。所以慢客户端丢事件（计数可在 /metrics 看到），而不是让所有人变慢。这是「隔离慢消费者」的经典做法。

### 10. `server/internal/cost/cost.go`、`metrics/metrics.go`、`jobs/jobs.go`

- **cost**：价格按「最长前缀匹配」（`claude-opus-5-5` 不会被 `claude-opus-5` 抢走；带日期后缀的 id 也能匹配）。**不认识的模型成本留 0 并计为 unpriced**，不瞎猜。人民币计价的模型用一个可配置汇率换成美元（这个汇率是配置项，不是测量值）。`config/prices.json` 与内置表由测试保证一致。
- **metrics**：手写的 Prometheus 文本格式（counter、回调 gauge、直方图），约 150 行。没用官方库是为了能讲清楚：直方图桶是累积的（`le="0.5"` 包含所有 ≤0.5 的观测）。
- **jobs**：retention 循环（默认关闭！一开始默认 30 天，结果服务一启动就把导入的旧 Claude 会话删了——发现后改成显式开启，这个踩坑可以讲）；检测循环只处理「有新数据且已安静 settle 秒」的 trace，避免 live 运行时每来一批就重跑检测器。

### 11. `server/cmd/loupe/serve.go` —— 组装与优雅关闭

**(a) 骨架**：解析参数 → 打开存储 → 建 broker、pipeline、metrics → 起 retention / 检测 / demo-live 后台任务 → 启动 HTTP → 等 SIGINT/SIGTERM。

**(b) 为什么——关闭顺序**：
1. 关闭 `done` channel，让 SSE 长连接自己退出（`http.Server.Shutdown` 会等所有连接空闲，而 SSE 永远不空闲，不先结束它就会卡死）；
2. `Shutdown` 停止接新连接、等在途请求完成；
3. `pipe.Close()` 把队列里已接受（返回过 202）的 span 全部写完；
4. 停后台任务，最后关数据库。
集成测试 `TestGracefulShutdownFlushesQueue` 真的编译二进制、发 3000 个 span（flush 间隔设成 30 秒让它们留在队列里）、挂一个 SSE 连接，然后发 SIGTERM，最后检查数据库里正好 3000 个。

### 12. `server/internal/claudeimport/*` —— Claude Code 转录导入器

**(a) 骨架**：`Discover` 找会话文件（主文件 + 新版的 `subagents/agent-*.jsonl`）；`readEntries` 逐行解析；`builder` 分组（主 agent / 各子 agent）→ 配对 tool_use 和 tool_result → 把子 agent 挂到派生它的 Task 调用下 → 生成 span。

**(b) 为什么**：
- **容错**：用 `bufio.Reader.ReadBytes` 而不是 `Scanner`（Scanner 默认单行上限 64KB，转录里一行可能有几 MB）；坏行计 `malformed`，**最后一行无换行且解析失败**计 `truncated`（文件正在被写/被截断）；不认识的 entry 类型按类型计数；所有统计在导入后打印——格式漂移一眼可见，而不是静默丢数据。
- **同一条 assistant 消息被拆成多行**（文字一行、tool_use 一行，`message.id` 相同、usage 重复）：按 `message.id` 合并，usage 取最后一行，否则 token 会翻倍。
- **子 agent 链接**按证据强弱三步：① Task 结果里的 `agentId`；② 子 agent 第一条用户消息 == Task 的 prompt；③ 时间窗口包含。都不行就挂到根下并计 `unlinked`。嵌套子 agent 自然支持。
- **确定性的 span id**（`root`、`tool:<tool_use_id>`、`llm:<message.id>`）：重复导入是幂等的；hook 脚本用同样的 id，所以「先实时 hook、后导入转录」会合并而不是重复。
- LLM span 的时长是近似值：从上一条记录到这条 assistant 记录的时间（转录里没有真正的请求开始时间）——写在局限性里。

### 13. `server/internal/detect/detect.go` + `eval.go` + `internal/corpus/corpus.go` —— 检测器与评测

**(a) 骨架**：按 agent 分组、按时间排序工具调用，跑 5 个检测器：
- **loop**：窗口内 ≥3 次「输入相似、输出也相似、中间没有编辑」的同一工具调用；
- **rework**：同一文件编辑 ≥5 次，或「改→测试失败→再改」循环 ≥3 次；
- **error_streak**：连续 ≥3 次工具失败；
- **stuck**：连续 ≥13 次只读/搜索调用，没有编辑也没跑测试；
- **budget_burn**：总 token 超预算，或最后 3 次调用的上下文是前 3 次的 ≥5 倍。

`corpus.go` 生成 300 条合成 trace（5 类故障 + 干净，带噪声、「刚好低于阈值」的难负例和检测器看不见的难正例）；`eval.go` 在 trace 级别算混淆矩阵、P/R/F1，并列出漏掉的变体和误报来源。

**(b) 为什么**：
- 相似度用 token 集合的 Jaccard，**数字统一归一成 `#`**：「读第 10–60 行」和「读第 12–62 行」被视为同一个动作——循环的 agent 常常就是这样翻页的。
- loop 要求**输出也相同、且中间没有编辑**：改完代码再跑测试是正常的修复，输出每次也不同——那叫 rework，不叫 loop。这条规则是在 train 集上看到大量误报后加的（过程写在 `results/detectors/README.md`）。
- **train/test 划分**按 trace id 哈希固定；阈值只看 train 调了两轮，test 只跑一次。
- **诚实的局限**：生成器和检测器是同一个人写的，所以分数衡量的是「检测器能不能抓住我建模的模式」，不是真实世界准确率。stuck 的召回低是因为生成器里有「写笔记打断只读序列」的难正例，这正是简单规则抓不住的情况。

### 14. `server/cmd/loupe/bench.go` + `scripts/bench.sh` —— 基准测试

- `loadgen` 预先编码好所有请求体（不把客户端 JSON 开销算进去），并发发送，记录 429；**吞吐算到服务器真正写完为止**（轮询 `/metrics` 的 written 计数），而不是算到收到 202——否则测的只是「进队列」的速度。
- `bench-query` 先生成约 100 万 span 的库（外加一条 1 万 span 的超大 trace 测最坏情况组装），再对每个接口顺序请求、预热 20 次、取 p50/p95/p99。
- `bench.sh` 每次跑都用新库、新服务器进程，重复 3 次，原始输出进 `results/bench/`。

---

## 第二部分：Python

### 15. `importers/claude_hook.py` —— 实时 hook 适配器

**(a) 骨架**：从 stdin 读 hook JSON → `build_spans()`（纯函数）→ POST 给 Loupe。PreToolUse 只记录开始时间到临时文件；PostToolUse 读回开始时间，生成带真实时长的 tool span。

**(b) 为什么**：hook 的第一原则是**绝不影响 Claude Code**：0.5 秒超时、永远 `exit 0`、**不往 stdout 写任何东西**（stdout 可能被当作 hook 的决策输出）、只用标准库。每个请求都带上 `root` span（服务器去重），所以中途装上 hook 也有根节点。

### 16. `sdk/python/loupe_sdk/` —— 打点 SDK

**(a) 骨架**：`Tracer` 提供 `agent()/llm()/tool()/span()` 上下文管理器和 `@trace` 装饰器；`Span` 能转成 OTLP JSON 或原生格式；`OTLPHttpExporter` 后台线程攒批发送。

**(b) 为什么**：
- 当前 span 存在 **`contextvars.ContextVar`** 里，而不是全局变量或 thread-local：线程和 asyncio 任务各自有独立的「当前 span」，并发的 agent 不会互相认错父节点（测试里两个协程同时跑来验证）。
- 导出器：`put_nowait` 放进有界队列，**满了就丢并计数，绝不阻塞 agent**；后台线程攒批，遇到 429/5xx 指数退避重试——和服务端背压配套。
- 属性名用 OTel GenAI 约定，所以同一套打点也能发给 Jaeger/Tempo 等任何 OTel 后端。
- `_SpanContext.__exit__` 遇到异常时把 span 标成 error 并记录异常类型，然后 `return False` **不吞异常**。

### 17. `agents/loupe_agents/` —— 手写的迷你 coding agent

- **`agent.py`**：最朴素的 agent 循环——调用模型；没有工具调用就结束；有就逐个执行并把结果作为 `role: tool` 消息追加。每一步都打点。`run_loop` 可以接着已有对话继续（多 agent 里 reviewer 打回时用）。
- **`tools.py`**：工作区是临时目录；`_path()` 用 `realpath` 检查路径不能逃出工作区（防 `../../etc/passwd`）；跑测试用子进程 + 超时，环境变量只留 PATH。工具出错**返回文本而不是抛异常**——工具失败是 agent 的正常经历，模型应该读到错误并作出反应。
- **`providers.py`**：OpenAI 兼容接口（默认 DashScope + qwen3.8-flash）。key **只在调用时**从 `DASHSCOPE_API_KEY` 或 `$LOUPE_KEYFILE` 指向的文件读取，不进代码、不进日志（测试验证错误信息里不含 key）。429/5xx 重试，4xx 不重试。
- **`ledger.py`**：花费账本。**每次调用前**用「估算的 prompt token（字符数/3，偏保守）+ 输出上限 1500」算最坏成本，超过上限就抛 `BudgetExceeded`，请求根本不发出；调用后记录真实用量。JSON 文件**原子写入**（写临时文件再 `os.replace`），崩溃也不会留下半个文件，续跑时知道已经花了多少。保证的边界：只有当某次调用真实 prompt token 超过估算时才可能超支，而且下一次检查立刻会拦住，所以超支最多一次调用。
- **`fake.py`**：确定性的「假模型」：按 (seed, task, arm) 播种的随机数决定是否循环读文件、写几次错误版本（返工）、是否放弃。它知道参考答案——所以它的通过率**毫无意义**，只用来跑通整条流水线。

### 18. `experiment/loupe_exp/` —— 预注册实验

- **`tasks.py`**：24 个自己写的小任务；`verify()` 保证每个参考答案通过、每个 stub 失败。
- **`arms.py`**：A 单 agent；B 先复述任务再写代码；C planner → coder → reviewer（最多 2 轮打回）。三组共用同一个 coder 提示词、工具和步数上限，唯一的差别是外层 harness。
- **`run.py`**：可续跑（结果逐行追加到 `runs.jsonl`，重启时跳过已完成的 key；崩溃留下的半行会被识别并另起一行）；按任务分组、每个任务内部打乱 arm 顺序（种子固定），避免某个 arm 总在最前/最后；预算耗尽退出码 3、没有 key 退出码 2。
- **`analyze.py`**：分析单位是任务（n=24）；每组通过率 + 自助法（bootstrap，10,000 次）95% CI；B−A、C−A 的配对差 + 配对 bootstrap CI + 胜/负/平；单次重复时附精确 McNemar 检验；token 比用几何平均。**只分析完整的重复轮次**，被预算截断的那一轮只报告不分析——这些都在 PREREG.md 里事先写死。

---

## 第三部分：前端（`web/`）

（见 `web/README.md`。重点会讲：`src/api.ts` 的类型化客户端；瀑布图里 bar 的 `left = (start - traceStart) / traceDuration`、`width = duration / traceDuration` 的布局计算；agent 树的自顶向下布局；Live 页 `EventSource` 在组件卸载时 `close()`，避免泄漏连接；Journey 页滚动动画用 IntersectionObserver。）

---

## 第四部分：面试问答（基于代码）

**Q1. 为什么用 SQLite 而不是 Postgres / ClickHouse？**
A：目标是「一条命令在笔记本上跑起来」的本地观测工具，SQLite 零运维。WAL 模式下一写多读正好匹配「一个攒批 writer + 很多查询」。代价：单写者上限、没有列存，聚合查询是全表扫描（`/stats` 在百万 span 下的耗时见 BENCHMARK.md）。数据量再大的话，存储层是 `store` 包一个接口的事，可以换 ClickHouse 这种列存。

**Q2. 你怎么实现背压？为什么不直接阻塞？**
A：`Pipeline.Submit` 用 CAS 在一个按 span 计数的额度上预留容量，超额立刻返回 `ErrQueueFull`，HTTP 层回 429 + `Retry-After: 1`。阻塞会让 handler goroutine 和 TCP 连接无限堆积，内存无界增长，最后所有人都超时。拒绝让内存有上界，压力回到客户端，客户端（SDK、导入器、loadgen）做退避重试。过载测试里我检查了「被接受的 span 数 == 服务器写入的 span 数」，即背压不丢已接受的数据。

**Q3. 202 和 200 有什么区别？**
A：默认 202 表示「已进入内存队列」，还没落盘——进程崩溃会丢这部分。需要确认落盘的调用方加 `?wait=true`，handler 会等这个 job 所在批次的事务提交后返回 200（或返回写入错误）。这是吞吐和持久性之间的显式取舍。优雅关闭时队列会被写完，所以正常停机不丢。

**Q4. 怎么保证幂等？**
A：主键 `(trace_id, span_id)` + `INSERT OR IGNORE`。关键是汇总表：我只用 `RowsAffected()==1` 的行计算增量，所以重试整批不会重复计 token/成本。导入器和 hook 用确定性的 span id，重复导入同一个会话也是幂等的。

**Q5. 乱序和迟到的 span 怎么处理？**
A：写入不依赖父节点存在；组装在查询时做。父节点没到的 span 作为孤儿根显示并标记；父节点到达后下一次查询自然挂上。汇总表的起止时间用 MIN/MAX 更新。检测器等 trace「安静」settle 秒后才跑；迟到的 span 会让 `last_ingest_ns > detected_ns`，于是重新检测。测试里把一条多 agent trace 打乱 200 次，组装结果完全一致。

**Q6. 为什么只有一个 writer goroutine？**
A：SQLite 同一时刻只允许一个写事务，多个 writer 只会抢锁、触发 busy 重试。单 writer + 攒批把每次提交的固定开销摊到上千个 span 上。代价是写吞吐受单核和单个事务限制——基准里报告了批量 vs 单条的差距。

**Q7. 攒批的两个参数怎么权衡？**
A：`MaxBatch` 越大摊薄越好，但单个事务越长、读者看到数据越晚；`FlushInterval` 是低流量时一个 span 最多等多久（默认 50ms）。高流量时是 MaxBatch 触发，低流量时是 interval 触发。

**Q8. SSE 慢客户端会拖慢 ingest 吗？**
A：不会。broker 对每个订阅者非阻塞发送，缓冲满了就丢弃并计数（`loupe_live_dropped_total`）。另外关闭时先关 `done` channel 让 SSE handler 退出，否则 `http.Server.Shutdown` 会一直等这些永不空闲的连接。

**Q9. 优雅关闭的顺序是什么？为什么？**
A：结束 SSE → Shutdown（停接新连接、等在途请求）→ 冲刷 ingest 队列 → 停后台任务 → 关库。反过来的话，比如先关库，队列里已经回过 202 的数据就丢了。有一个集成测试真的发 SIGTERM 验证。

**Q10. 如何避免在 /metrics 上产生高基数标签？**
A：标签只用路由类别（ingest/query/live/ops/static）和状态码，不用原始路径（里面有 trace id）。每个标签组合都是一个时间序列，原始路径会让序列数随 trace 数线性增长。

**Q11. OTLP 接入时踩过什么坑？**
A：OTLP/JSON 里 trace/span id 是十六进制字符串（protobuf 里是 bytes），int64 被编码成字符串，状态码可能是数字 2 也可能是枚举字符串 `STATUS_CODE_ERROR`。我写了 `flexInt` 和 `isErrorStatus` 兼容这些情况。protobuf 编码没支持，返回 415。

**Q12. Claude Code 的转录格式不稳定，你怎么保证导入器不崩？**
A：只依赖少数字段，其余忽略；坏行、截断行、未知类型、缺失的 tool_result、找不到 tool_use 的孤儿 result、未能链接的子 agent 全部计数并打印。超长行用 `ReadBytes`。测试夹具覆盖：截断的最后一行、中间的垃圾行、错误时间戳、未知字段、内联 sidechain（旧格式）、`subagents/` 目录（新格式）、嵌套子 agent。

**Q13. 子 agent 怎么挂到父 agent 上？**
A：三步证据：Task 结果里的 agentId（最强）→ 子 agent 首条消息等于 Task 的 prompt → 时间窗口包含（选开始最晚的那个未匹配 Task，适合嵌套）。都失败挂到根并计 `subagents_unlinked`，不猜。

**Q14. 为什么同一条 assistant 消息会出现多行？你怎么处理 token？**
A：Claude Code 把一次回复的各个内容块（文字、tool_use）分别写成一行，`message.id` 相同、usage 重复。如果每行都算，token 会翻倍。我按 message.id 合并成一个 LLM span，usage 取最后一行。测试 `TestBasicSession` 检查输出 token 是 40 而不是 5+40。

**Q15. loop 检测器怎么区分「循环」和「正常重复」？**
A：三个条件：输入相似（数字归一后的 Jaccard ≥0.8）、输出也相似、两次之间没有编辑。改代码后重跑测试是正常修复，输出也会变——归 rework。这条「中间没有编辑」规则是看 train 集误报加的。

**Q16. 检测器的 P/R 可信吗？**
A：只在合成语料上有意义。生成器和检测器是我一个人写的，存在循环论证的风险；held-out 只能防止对具体样本过拟合，不能证明真实世界的效果。所以 README 明确写「synthetic」，下一步是导入我自己的 Claude Code 历史，人工标注一部分做真实评测。

**Q17. stuck 检测召回为什么低？**
A：生成器里有一种难正例：agent 每读 7 个文件写一次 NOTES.md。写操作打断了「连续只读」序列，规则就看不见了。这暴露了简单规则的局限——真正要判断的是「有没有朝目标前进」，需要语义信息（比如测试通过数有没有增加）。

**Q18. Python SDK 为什么用 contextvars？**
A：要找「当前 span」作为父节点。全局变量在多线程下会串；thread-local 在 asyncio 里不行（多个协程在同一线程）。ContextVar 每个线程、每个 asyncio Task 都有自己的值，`reset(token)` 还能精确恢复上一层。

**Q19. SDK 的导出器在服务器挂掉时会怎样？**
A：不会阻塞业务代码：`export()` 是 `put_nowait`，队列满了就丢并计数；后台线程重试几次后放弃这一批并计入 `failed`。`shutdown()` 会在超时内尽量冲刷。测试里对一个没人监听的端口发 200 个 span，验证不阻塞且有丢弃计数。

**Q20. 你的预算控制能保证不超支吗？**
A：每次调用前按最坏情况检查（估算 prompt + 最大输出），超了就不发请求。唯一可能超支的情况是某次调用的真实 prompt token 超过估算（我用字符数/3，比通常的 /4 保守）；记录后下一次检查立即拦截，所以最多超出一次调用。账本原子写入，续跑时继承已花费金额，降低 cap 也立即生效。

**Q21. 实验为什么要预注册？分析为什么以任务为单位？**
A：先写死假设、指标、分析方法和停止规则，再看数据，避免「看了结果再挑分析方法」。同一个任务在三组里都跑，任务难度是最大的噪声来源，配对比较可以把它消掉；以任务为单位做 bootstrap，n=24，而不是把 216 次运行当成独立样本（那会低估方差）。

**Q22. 只有 24 个任务，统计功效够吗？**
A：不够检测小效应，这在 PREREG 里写明了——CI 会很宽，结论大概率是「不确定」，或者因为题目太简单出现天花板效应。这本身也是有用的结论：在简单任务上额外的 harness 步骤主要是花 token。

**Q23. 为什么 B 组的复述作为独立的一次调用，而不是写在 system prompt 里？**
A：要测的是「先让模型把任务说一遍」这个 harness 步骤本身。独立调用让复述成为一个可观测的 step span，token 成本可以单独统计；coder 的提示词三组完全相同，只多了一段复述文本，变量更干净。

**Q24. 假模型（FakeProvider）有什么用？它的数字能说明什么？**
A：它实现和真实 provider 一样的接口，所以 agent 循环、工具、打点、账本钩子、续跑、分析、UI 全部真实运行，只有「思考」是脚本。它知道参考答案，所以它的通过率**什么也说明不了**；报告和 UI 都打了醒目的 DRY RUN 标签。唯一用到它的数字是「规划用的 token 估算」，也明确标注了只是规划数字。

**Q25. 如果要上生产/多机，你会改什么？**
A：① ingest 前面加持久化队列（Kafka/Redpanda）或 WAL，让 202 也不丢；② 存储换成列存（ClickHouse），span 按时间分区，聚合走物化视图；③ 支持 OTLP protobuf 和 gRPC；④ 检测器变成消费者组，按 trace id 分片；⑤ 多租户与鉴权（现在默认只监听 127.0.0.1）。

**Q26. 这个项目里你最想重做的一个决定？**
A：retention 一开始默认 30 天——对「导入历史数据」的工具来说，这意味着一启动就删用户数据。修复成默认关闭后，我意识到默认值要按「最坏的意外」来选。另一个是导入器里 root span 的结束时间：会话还在进行时导入，root span 的 end 不会随重新导入更新（因为幂等插入），汇总表是对的，但瀑布图里 root 条会偏短。
