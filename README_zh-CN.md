# Env - Go 高性能环境变量库

[![Go Reference](https://pkg.go.dev/badge/github.com/cybergodev/env.svg)](https://pkg.go.dev/github.com/cybergodev/env)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Security Policy](https://img.shields.io/badge/security-policy-blue.svg)](docs/SECURITY.md)
[![Thread Safe](https://img.shields.io/badge/thread%20safe-%E2%9C%93-brightgreen.svg)](docs/CONCURRENCY_SAFETY.md)

**[English Documentation](README.md)** | **[www.cybergo.dev/env](https://www.cybergo.dev/env/)**

---

## 📋 概述

**Env** 是一个生产级零依赖、线程安全的 Go 环境变量管理库，专注于**安全性**、**并发性**和**开发者体验**。

### ✨ 核心特性

| 特性 | 描述 |
|:-----|:-----|
| 🚀 **一行配置** | `env.Load(".env")` 即可加载并应用到 `os.Environ` |
| 🔒 **类型安全** | `GetString`、`GetInt`、`GetBool`、`GetDuration`、`GetSlice[T]` |
| 📁 **多格式支持** | 自动检测 `.env`、`.json`、`.yaml` 文件 |
| ⚡ **线程安全** | 分片存储（32 分片）+ RWMutex，高并发优化 |
| 🛡️ **内存安全** | `SecureValue` 敏感数据自动清零，支持内存池 |
| 🔄 **变量展开** | 完整支持 `${VAR}` 语法及默认值 |
| 📝 **审计日志** | 内置 JSON/Log/Channel 处理器，满足合规需求 |
| 🧪 **测试支持** | 隔离加载器，确保测试隔离性 |
| 📦 **零依赖** | 仅使用标准库 |

---

## 📦 安装

```bash
go get github.com/cybergodev/env
```

**要求：** Go 1.25+

---

## 🚀 快速开始

### 第一步：创建 `.env` 文件

```env
# 应用配置
APP_NAME=myapp
APP_PORT=8080
DEBUG=true

# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_PASSWORD=secret123

# 超时设置
TIMEOUT=30s
```

### 第二步：在 Go 代码中使用

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/cybergodev/env"
)

func main() {
    // 一行初始化 - 加载并应用到 os.Environ
    if err := env.Load(".env"); err != nil {
        log.Fatalf("加载失败: %v", err)
    }

    // 类型安全访问（支持默认值）
    port    := env.GetInt("APP_PORT", 8080)
    debug   := env.GetBool("DEBUG", false)
    timeout := env.GetDuration("TIMEOUT", 30*time.Second)

    fmt.Printf("服务: %s:%d\n", env.GetString("APP_NAME", "unknown"), port)
    fmt.Printf("调试: %v, 超时: %v\n", debug, timeout)
}
```

---

## 📚 使用指南

### 基础操作

```go
// 多文件初始化（默认配置下先加载的值生效；
// 需要后加载的文件覆盖时，用下面的 Overload）
if err := env.Load(".env", "config.json", ".env.local"); err != nil {
    log.Fatal(err)
}

// 或者：后加载的文件覆盖先前的，且覆盖进程已有环境变量
// （godotenv Overload 惯例——若部署平台注入的变量必须保持优先于文件值，
// 请改用 Load）
if err := env.Overload("config.json", "config.yaml"); err != nil {
    log.Fatal(err)
}

// 检查存在性
value, exists := env.Lookup("KEY")
if !exists {
    // 处理缺失的键
}

// CRUD 操作
if err := env.Set("KEY", "value"); err != nil { /* 处理错误 */ }
if err := env.Delete("KEY"); err != nil { /* 处理错误 */ }
keys  := env.Keys()               // 获取所有键
all   := env.All()                // 获取所有变量为 map
count := env.Len()                // 变量数量
```

> **注意：** 包级函数（`GetString`、`Set`、`Lookup` 等）必须先调用 `Load()`、
> `Overload()` 或 `LoadWithConfig()` 才能使用。未初始化时，`Get*`/`Lookup`/`Keys`/`All`/`Len`/`GetSecure`
> 会静默返回默认值/零值，而 `Set`、`Delete`、`Validate`、`ParseInto` 会返回 `ErrNotInitialized`。
> `Load()` 只能初始化默认加载器一次（三个初始化函数共享同一个默认加载器），
> 重复调用返回 `ErrAlreadyInitialized`；
> 如需重新初始化（例如测试之间），请先调用 `ResetDefaultLoader()`。

### 重复键与合并顺序

**跨文件**（`.env`/`.json`/`.yaml` 任意混用——嵌套键会展平到统一的大写键空间，
`.env` 里的 `DATABASE_HOST` 与 YAML 里的 `database: {host: ...}` 是同一个键）：
按文件列表顺序加载。默认（`OverwriteExisting = false`）**先加载的文件优先**，
后加载的同名键被跳过；设为 `true` 时后加载的覆盖先前的。包级 `Overload`
初始化函数即该设置的快捷方式（godotenv 惯例）——注意它同时会覆盖进程
已有环境变量，而 `Load` 永远不会。

**同一文件内**，默认的重复键解析规则因格式而异：

| 格式 | 文件内重复 `KEY` 的结果 |
|------|--------------------------|
| `.env` | `OverwriteExisting=false`（默认）首个生效；`true` 时后者生效 |
| YAML | 恒为后者生效（与 `OverwriteExisting` 无关） |
| JSON | 恒为后者生效（`encoding/json` 解码顺序，与 `OverwriteExisting` 无关） |

可配置 `DuplicateKeyPolicy` 统一或收紧该行为：

```go
cfg := env.DefaultConfig()
cfg.DuplicateKeyPolicy = env.DuplicateKeyError // 重复键直接报错（匹配 env.ErrDuplicateKey）
// 其他取值：DuplicateKeyFirstWins / DuplicateKeyLastWins / DuplicateKeyInherit（默认）
```

说明：

- `DuplicateKeyFirstWins` 适用于 `.env` 与 YAML。对 JSON 数据（含 YAML 内联 JSON），
  **确实含重复键**的文档会被拒绝——`encoding/json` 解码时即丢弃先出现的键，无法
  兑现该策略；无重复键的 JSON 文档正常加载（其在任何策略下解析结果相同）。
  `DuplicateKeyError` 模式（及 FirstWins 的含重复键判定）下，JSON 重复键由保守
  预扫描按原始字节比较检测；同一键的转义等价拼写不会被检出。
- **扁平键碰撞恒为错误**（与策略无关）：两个不同源路径展平到同一个键
  （`a_b: 1` 与 `a: {b: 2}` 并存、字面 `a_0` 与数组 `a`、仅大小写不同的两个键），
  此前按 map 遍历顺序随机定胜者，跨运行结果不确定；现在以 `env.ErrDuplicateKey` 报错。
- **大小写：** YAML/JSON 键展平时统一转大写；`.env` 键按原样存储，因此仅大小写
  不同的键跨格式不会碰撞、会共存。查找时先精确匹配再尝试大写形式（见 `Lookup`）。

### 类型访问

```go
// 字符串（带默认值）
name := env.GetString("APP_NAME", "default-app")

// 整数（返回 int64）
port := env.GetInt("PORT", 8080)

// 布尔值 - 支持: true/1/yes/on/enabled, false/0/no/off/disabled
debug := env.GetBool("DEBUG", false)

// 时间间隔
timeout := env.GetDuration("TIMEOUT", 30*time.Second)

// 浮点数（返回 float64）
ratio := env.GetFloat64("RATIO", 0.5)

// 无符号整数（返回 uint64）
maxSize := env.GetUint64("MAX_SIZE", 1024)

// 泛型切片: string, int, int64, uint, uint64, bool, float64, time.Duration
hosts := env.GetSlice[string]("HOSTS", []string{"localhost"})
ports := env.GetSlice[int]("PORTS", []int{8080})
```

### 灵活键查找

所有 `Get*` 方法均支持**不区分大小写**和**点号语法**的键解析，你可以用最符合代码风格的方式访问值：

```go
// 已知 .env: DEEPSEEK_KEY=sk-abc123

// 不区分大小写查找（大写回退）
env.GetString("DEEPSEEK_KEY")   // "sk-abc123" — 精确匹配
env.GetString("deepseek_key")   // "sk-abc123" — 大写回退匹配
env.GetString("DeepSeek_Key")   // "sk-abc123" — 大写回退匹配

// 点号语法（点号 → 下划线，自动转大写）
env.GetString("deepseek.key")   // "sk-abc123" — 解析为 DEEPSEEK_KEY
env.GetString("db.host")        // 解析为 DB_HOST

// 下划线与点号混用
env.GetString("test_app.key")   // 解析为 TEST_APP_KEY

// 适用于所有 Get* 方法
env.GetInt("app.port")          // 解析为 APP_PORT
env.GetBool("debug.mode")       // 解析为 DEBUG_MODE

// 逗号分隔值的索引访问（虚拟查找）
// 已知 .env: LIST=a,b,c
env.GetString("LIST.0")         // "a" — 第一个元素
env.GetString("list.2")         // "c" — 第三个元素（基键不区分大小写）
```

**解析策略：**

| 步骤 | 简单键（无点号） | 点号语法键 |
|:-----|:----------------|:-----------|
| 1 | 精确匹配 | 点号转下划线，转大写 → 查找 |
| 2 | 大写回退 | （已在步骤 1 完成） |
| 3 | — | 键以数字索引结尾时：解析基键，按索引切分逗号分隔的值 |

**最佳实践：** 在 `.env` 文件中使用 `UPPER_SNAKE_CASE`（大写下划线格式），确保所有查找方式均可正确工作。

### 结构体映射

```go
type Config struct {
    Port    int           `env:"PORT,envDefault:8080"` // 内联默认值
    Debug   bool          `env:"DEBUG" envDefault:"false"` // 独立标签 — 两种写法均支持
    Timeout time.Duration `env:"TIMEOUT"`
    Origins []string      `env:"CORS_ORIGINS"`
}

var cfg Config
if err := env.Load(".env"); err != nil {
    log.Fatal(err)
}
if err := env.ParseInto(&cfg); err != nil {
    log.Fatal(err)
}
```

### Loader API（精细控制）

```go
cfg := env.ProductionConfig()
cfg.Filenames = []string{"app.env"} // 仅允许相对路径；绝对路径会被拒绝

loader, err := env.New(cfg)
if err != nil {
    log.Fatal(err)
}
defer loader.Close()

// 加载额外文件
if err := loader.LoadFiles("override.env"); err != nil {
    log.Fatal(err)
}

// 应用到 os.Environ
if err := loader.Apply(); err != nil {
    log.Fatal(err)
}

// 访问值
port := loader.GetInt("PORT", 8080)
```

---

## 📁 多格式支持

### .env 文件

```env
# 注释以 # 开头
DATABASE_URL=postgres://localhost:5432/db
PORT=8080
DEBUG=true

# 引号可选
MESSAGE="Hello World"
SINGLE='单引号也可以'

# 变量展开
HOST=localhost
URL=${HOST}:${PORT:-443}
```

### JSON（自动扁平化）

```json
{
    "database": { "host": "localhost", "port": 5432 },
    "ports": [8080, 8081]
}
```

**访问方式：**
```go
env.GetString("database.host")    // "localhost"（点号语法）
env.GetInt("database.port")       // 5432
env.GetSlice[int]("ports")        // [8080, 8081]
// 也可以用: DATABASE_HOST, DATABASE_PORT
```

### YAML（自动扁平化）

```yaml
database:
  host: localhost
  port: 5432
ports: [8080, 8081]
```

**访问方式：** 与 JSON 相同 - 使用点号语法或大写下划线格式。

> **值中含 `": "` 时必须加引号。** 未加引号的 YAML 纯量值不能包含「冒号+空白」——
> YAML 会把它读成嵌套键。这类行现在会以 `YAMLError` 报错，而不是被静默拆成两个变量。
> 用 `'...'` 或 `"..."` 包住即可保留原值：
>
> ```yaml
> # 报错："Expires: ..." 会被当成第二个键
> apnews.com: sid=1; Expires: Wed, 21 Oct 2015 07:28:00 GMT
>
> # 正确
> apnews.com: 'sid=1; Expires: Wed, 21 Oct 2015 07:28:00 GMT'
> ```
>
> 行内 JSON / flow 区（`{...}`、`[...]`）不受影响：其中的 `": "` 是数据，
> `g_state={"i_l": 0}`、`{"a": "b: c"}` 均原样读取。空键、裸标量行、
> 闭合引号之后的残留文本同样会报错，不再静默丢弃。
> 以 `{`/`[` 开头且构成完整 flow 集合的值会展开为前缀/下标键
> （`k: {"a": 1}` → `K_A`）；若要保留为一个字符串，请加引号。

---

## 🔄 序列化 / 反序列化

```go
// Map 转格式字符串
data := map[string]string{"PORT": "8080", "DEBUG": "true"}

envString, _  := env.Marshal(data)                      // .env（默认）
jsonString, _ := env.Marshal(data, env.FormatJSON)      // JSON
yamlString, _ := env.Marshal(data, env.FormatYAML)      // YAML

// 字符串解析为 Map
m, _ := env.UnmarshalMap("PORT=8080\nDEBUG=true")           // .env 格式
m, _ := env.UnmarshalMap(`{"port": 8080}`, env.FormatJSON)  // JSON
m, _ := env.UnmarshalMap(yamlString, env.FormatAuto)        // 自动检测

// Struct <-> Map 转换
m, _ := env.MarshalStruct(&config)          // 结构体转 map
env.UnmarshalInto(m, &config)               // map 转结构体

// 字符串直接转结构体
env.UnmarshalStruct("PORT=8080\nDEBUG=true", &config, env.FormatEnv)
```

> **Marshal 会拒绝加载器本就拒绝的键。** 输出文档必须能被本库自身加载，
> 因此当键不被对应读取器接受时，`Marshal` 返回 `*MarshalError`：
> `.env` 输出要求 `.env` 键语法（`^[A-Za-z][A-Za-z0-9_]*$`），
> YAML/JSON 输出要求展平键空间（字母、数字、`_`、`@`、`-`、`.`）。
> 变量表中可以合法存在更宽的键（`Loader.Set` 接受带点键名，YAML 加载会存下
> `APNEWS.COM` 这类字面键），此时请选择读取器接受的格式，或重命名该键。
> 用 `env.IsMarshalError(err)` 判断错误类型。

---

## 🔄 变量展开

`.env` 文件完整支持 `${VAR}` 语法：

```env
HOST=localhost
PORT=8080

# 变量引用
URL=${HOST}:${PORT}                    # → "localhost:8080"

# 未设置时使用默认值（空字符串会被保留）
TIMEOUT=${TIMEOUT:-30s}

# 未设置时赋值默认值 — 赋值结果成为真实变量，
# 对同文件中后续引用以及 Get/Apply 可见
NAME=${NAME:=default_value}

# 必需变量 — 未设置或为空时报错
API_KEY=${API_KEY:?API_KEY must be set}

# 组合展开
FULL_URL=https://${HOST}:${PORT:-443}
```

与 POSIX shell 行为的两处刻意差异:

- **`EMPTY` 设为空字符串时,`${EMPTY:-default}` 保留空字符串**。`:-` 默认值仅在变量**未设置**时生效(空字符串是有效的显式值),而 POSIX 中 `:-` 对空值同样生效。
- **`${}` 展开为字面量 `{}`**,而不是像 bash 那样报 "bad substitution" 错误。

`:=` 赋值语义:文件加载期间,展开后的默认值会赋给该变量 —— 对本次遍历中排在后面的键的引用可见(键按排序顺序处理,结果因此是确定性的),并且像普通解析变量一样出现在加载结果 map 中。已经设置过的变量(文件内或进程环境中)不会被重新赋值。

---

## 🔒 安全值处理

使用 `SecureValue` 处理密码、API 密钥、令牌等敏感数据：

```go
// 获取 SecureValue
sv := env.GetSecure("API_KEY")
if sv != nil {
    defer sv.Release()

    // 安全日志输出（String() 返回掩码表示）
    // 输出格式：[SECURE:<N> bytes]
    // 启用内存锁定时，会追加状态后缀：
    //   [SECURE:32 bytes locked] / [SECURE:32 bytes unlocked] / [SECURE:32 bytes lock-failed]
    fmt.Println(sv)                // [SECURE:32 bytes]
    fmt.Println(sv.Masked())       // [SECURE:32 bytes]

    // 访问实际值（请谨慎使用！）
    value := sv.Reveal()

    // 获取字节（调用者必须清理）
    data := sv.Bytes()
    defer env.ClearBytes(data)     // 手动清零
}

// 直接创建 SecureValue
secret := env.NewSecureValue("super_secret")
defer secret.Release()

// 创建并检查错误（严格模式）
secret, err := env.NewSecureValueStrict("super_secret")
if err != nil {
    log.Fatal("内存锁定失败:", err)
}
defer secret.Release()
```

### SecureValue 方法

| 方法 | 描述 |
|:-----|:-----|
| `String()` | 获取掩码表示（可用于 `fmt.Printf`、日志输出） |
| `Reveal()` | 获取明文值（**请谨慎使用！**） |
| `Bytes()` | 获取字节切片副本（调用者必须清理） |
| `Length()` | 获取值长度（不暴露值本身） |
| `Masked()` | 获取掩码表示用于日志 |
| `Close()` | 清零内存，不归还到池 |
| `Release()` | 清零内存并归还到池 |
| `IsClosed()` | 检查是否已关闭 |
| `IsMemoryLocked()` | 检查内存是否受保护（防止交换到磁盘） |
| `MemoryLockError()` | 获取内存锁定失败的错误信息 |

---

## 📝 审计日志

```go
auditFile, err := os.Create("audit.log")
if err != nil {
    log.Fatal(err)
}
defer auditFile.Close()

cfg := env.ProductionConfig()
cfg.AuditEnabled = true
// 请使用文件而非 os.Stdout：loader.Close() 会关闭处理器，进而关闭底层 writer
cfg.AuditHandler = env.NewJSONAuditHandler(auditFile)

loader, err := env.New(cfg)
if err != nil {
    log.Fatal(err)
}
defer loader.Close()

if err := loader.LoadFiles("app.env"); err != nil {
    log.Fatal(err)
}
// 输出: {"timestamp":"...","action":"parse","reason":"parsed: app.env","success":true,"duration_ns":150000}
//       {"timestamp":"...","action":"set","key":"[MASKED]","reason":"loaded","success":true,"masked":true}
//
// 敏感键（如 API_KEY）在审计输出中会被掩码为 [MASKED]。
```

**内置处理器：**

```go
env.NewJSONAuditHandler(w)              // JSON 格式 → io.Writer
env.NewLogAuditHandler(logger)          // 标准 log.Logger
env.NewChannelAuditHandler(ch)          // 通道（外部处理）
env.NewCloseableChannelHandler(size)    // 自有缓冲通道，带生命周期管理
env.NewNopAuditHandler()                // 空操作（丢弃）
```

---

## 🧪 测试支持

```go
func TestConfig(t *testing.T) {
    // 创建隔离加载器（不影响全局状态）
    cfg := env.TestingConfig()
    cfg.Filenames = []string{".env.test"}

    loader, err := env.New(cfg)
    if err != nil {
        t.Fatal(err)
    }
    defer loader.Close()

    port := loader.GetInt("PORT", 8080)
    // 测试你的代码...
}

// 测试之间重置默认加载器（若已调用过 Load() 则必须先重置）
func TestMain(m *testing.M) {
    if err := env.ResetDefaultLoader(); err != nil {
        log.Printf("重置默认加载器: %v", err)
    }
    os.Exit(m.Run())
}
```

---

## 🛠️ 工具函数

```go
// 敏感键检测
env.IsSensitiveKey("API_SECRET")  // true
env.IsSensitiveKey("HOST")        // false

// 值掩码 — 敏感键返回 "[MASKED]"；
// 非敏感值 ≤ 20 字符原样返回，超长则截断并追加 "..."
env.MaskValue("API_KEY", "secret123")  // "[MASKED]"

// 键掩码用于日志 — 敏感键返回固定 token "[MASKED]"
env.MaskKey("DB_PASSWORD")  // "[MASKED]"

// 日志安全处理
safe := env.SanitizeForLog(userInput)

// 掩码字符串中的敏感内容
masked := env.MaskSensitiveInString(logMessage)

// 格式检测
env.DetectFormat("config.yaml")  // FormatYAML（String() → "yaml"）
```

### 敏感键模式

自动检测的模式（不区分大小写）：

```
password, secret, token, credential, passphrase,
api_key, apikey, access_key, secret_key, private_key,
private, auth, session, cookie,
encryption_key, signing_key, connection_string,
database_url, db_password, ssn, credit_card,
mnemonic, seed, recovery, wallet, service_account
```

> **注意：** 以上为代表性子集，完整列表包含 50 个模式。
> 匹配不区分大小写；绝大多数模式按子串匹配（例如 `MY_API_KEY` 因包含 `API_KEY` 而匹配），
> 但 `AUTH`、`PAN`、`PWD`、`USERNAME` 四个短模式仅在标识符边界匹配，
> 因此 `AUTHOR`、`EXPAND` 这类键不会被误判为敏感。

---

## ⚙️ 配置选项

### 预设配置

```go
env.DefaultConfig()     // 安全默认值
env.DevelopmentConfig() // 宽松限制 + 允许覆盖
env.TestingConfig()     // 紧凑配置 + 隔离测试
env.ProductionConfig()  // 严格安全 + 审计
```

### 配置对比

| 设置 | Default | Development | Testing | Production |
|------|---------|-------------|---------|------------|
| `FailOnMissingFile` | false | false | false | **true** |
| `OverwriteExisting` | false | **true** | **true** | false |
| `ValidateValues` | **true** | **true** | **true** | **true** |
| `AuditEnabled` | false | false | false | **true** |
| `MaxFileSize` | 8 MB | 10 MB | 64 KB | 64 KB |
| `MaxVariables` | 1,000 | 1,000 | 50 | 50 |

### 完整配置选项

```go
cfg := env.DefaultConfig()

// === 文件处理（FileConfig）===
cfg.Filenames         = []string{".env"}
cfg.FailOnMissingFile = false
cfg.OverwriteExisting = true
cfg.AutoApply         = true

// === 验证（ValidationConfig）===
cfg.RequiredKeys   = []string{"DB_URL"}
cfg.AllowedKeys    = []string{"PORT", "DEBUG"}  // 空 = 允许所有
cfg.ForbiddenKeys  = []string{"PATH"}           // 阻止危险键
cfg.ValidateValues = true
cfg.ValidateUTF8   = true
cfg.KeyPattern     = nil                  // nil = DefaultKeyPattern

// === 安全限制（LimitsConfig）===
cfg.MaxFileSize       = 8 << 20   // 8 MB
cfg.MaxVariables      = 1000
cfg.MaxLineLength     = 8192
cfg.MaxKeyLength      = 128
cfg.MaxValueLength    = 8192
cfg.MaxExpansionDepth = 5

// === 解析（ParsingConfig）===
cfg.AllowExportPrefix  = true                     // 允许 "export KEY=value" 语法
cfg.AllowYamlSyntax    = false                    // .env 中的 YAML 风格值
cfg.ExpandVariables    = true                     // 展开 ${VAR}(仅 .env;JSON/YAML 不展开)
cfg.DuplicateKeyPolicy = env.DuplicateKeyInherit  // Inherit / FirstWins / LastWins / Error
cfg.ExpansionScope     = env.ExpansionFileOnly    // ${VAR} 仅从文件内变量解析(SEC-03)

// === JSON 选项（JSONConfig）===
cfg.JSONNullAsEmpty    = true
cfg.JSONNumberAsString = true
// cfg.JSONBoolAsString — 已弃用，无效果：JSON 布尔值始终按规范渲染
// "true"/"false"。保持默认值 false。
cfg.JSONMaxDepth       = 16

// === YAML 选项（YAMLConfig）===
cfg.YAMLNullAsEmpty    = true
cfg.YAMLNumberAsString = true
cfg.YAMLBoolAsString   = true
cfg.YAMLMaxDepth       = 16

// === 高级选项（ComponentConfig）===
cfg.Prefix          = "APP_"  // 仅加载带前缀的键
cfg.FileSystem      = nil     // nil = 操作系统文件系统
cfg.CustomValidator = nil     // 替换内置键/值验证器
cfg.CustomExpander  = nil     // 替换内置变量展开器
cfg.CustomAuditor   = nil     // 替换内置审计日志器

// === 审计日志（ComponentConfig）===
cfg.AuditEnabled = true
cfg.AuditHandler = env.NewJSONAuditHandler(os.Stdout) // 仅演示；生产环境请用文件 — Close() 会关闭底层 writer
```

> **注意：** Config 使用嵌套结构体（`FileConfig`、`ValidationConfig`、`LimitsConfig`、
> `JSONConfig`、`YAMLConfig`、`ParsingConfig`、`ComponentConfig`）并通过字段提升访问。
> 你可以直接访问字段（`cfg.Filenames`）或显式访问（`cfg.FileConfig.Filenames`）。

> **配置风格（API 契约）：** `Config` 结构体是唯一的配置通道——本库没有函数选项，
> 每个字段都在 `DefaultConfig()` 中有显式默认值。零值语义：全零的 `Config{}`
> 会被 `New` 当作 `DefaultConfig()`；部分填写的 Config **不会**被自动补全
> （未设置的限额会让 `Validate` 以 `ErrInvalidConfig` 拒绝）——始终从
> `DefaultConfig()` 或某个预设出发。少数进程级开关刻意不放进 Config：
> `SetMemoryLockEnabled` / `SetMemoryLockStrict`、`RegisterParser` /
> `ForceRegisterParser` 以及 `DefaultFileSystem` 变量——请在程序初始化阶段设置。

### 默认限制

| 设置 | 默认值 | 硬限制 |
|------|--------|--------|
| MaxFileSize | 8 MB | 100 MB |
| MaxLineLength | 8,192 字符 | 64 KB |
| MaxKeyLength | 128 字符 | 1,024 字符 |
| MaxValueLength | 8,192 字符 | 1 MB |
| MaxVariables | 1,000 | 10,000 |
| MaxExpansionDepth | 5 | 20 |
| JSONMaxDepth / YAMLMaxDepth | 16 | 100 |

> **行长限制的适用范围：** `MaxLineLength` 对所有格式生效，包括 JSON 与 YAML。
> 压缩为单行的 JSON（minified）在检查中算作一行，必须落在该限制内——其硬上限
> 为 64 KB，因此更大的单行压缩文档无法仅靠调大配置加载，请改为带换行的格式化输出。
> `MaxVariables` 在 `.env` 解析中途强制；JSON/YAML 在解析并展平后按扁平键数量检查
> （解析期的内存上界由 YAML token / JSON 节点数上限承担）。

---

## ⚠️ 错误处理

库提供哨兵错误（配合 `errors.Is`）和结构化错误类型（配合 `errors.As`）：

```go
if err := env.Load(".env"); err != nil {
    switch {
    case errors.Is(err, env.ErrFileNotFound):
        // 文件缺失 — 提供回退或快速失败
    case errors.Is(err, env.ErrFileTooLarge):
        // 超过 cfg.MaxFileSize
    case errors.Is(err, env.ErrSecurityViolation):
        // 禁止键或安全策略违规
    }
}

var parseErr *env.ParseError
if errors.As(err, &parseErr) {
    fmt.Printf("%s:%d: %v\n", parseErr.File, parseErr.Line, parseErr.Err)
}
```

**哨兵错误：** `ErrFileNotFound`、`ErrFileTooLarge`、`ErrLineTooLong`、`ErrInvalidKey`、
`ErrForbiddenKey`、`ErrSecurityViolation`、`ErrInvalidValue`、`ErrMissingRequired`、
`ErrMaxVariables`、`ErrExpansionDepth`、`ErrClosed`、`ErrInvalidConfig`、
`ErrNotInitialized`、`ErrAlreadyInitialized`、`ErrDuplicateKey`、`ErrAuditFailed`

**结构化错误类型：** `ParseError`（文件/行号）、`ValidationError`、`SecurityError`、
`FileError`、`ExpansionError`、`JSONError`、`YAMLError`、`MarshalError` —
可用 `env.IsMarshalError(err)` 判断。

可运行示例见 [examples/09_error_handling](examples/09_error_handling/)。

---

## 📖 API 参考

两层 API 互为镜像：下表中的每个包级函数都是同名 `Loader` 方法的薄代理，
签名一致（`env.GetString` ↔ `loader.GetString`、`env.Set` ↔ `loader.Set` 等）。
例外：`Load` / `Overload` / `LoadWithConfig` 初始化共享的默认加载器
（实例层对应 `New`）；`GetSlice` 与 `GetSliceFrom` 成对（Go 方法不支持
类型参数）；无状态的一次性解析 / 序列化 / 工具 / 内存锁 / 解析器注册族
在设计上没有实例层对应。

### 包函数

| 函数 | 说明 |
|:-----|:-----|
| `Load(files...)` | 加载文件并应用到 `os.Environ`（仅可调用一次，见上文说明） |
| `Overload(files...)` | 同 `Load`，但后加载的文件覆盖先前的，且覆盖进程已有环境变量 |
| `LoadWithConfig(cfg)` | 使用自定义配置加载（强制 `AutoApply = true`） |
| `GetString(key, def...)` | 获取字符串值 |
| `GetInt(key, def...)` | 获取 `int64` 值 |
| `GetBool(key, def...)` | 获取布尔值 |
| `GetDuration(key, def...)` | 获取时间间隔值 |
| `GetFloat64(key, def...)` | 获取 `float64` 值 |
| `GetUint64(key, def...)` | 获取 `uint64` 值 |
| `GetSlice[T](key, def...)` | 获取泛型切片 |
| `GetSliceFrom[T](loader, key, def...)` | 从指定加载器获取切片 |
| `Lookup(key)` | 获取值 + 存在性检查 |
| `Set(key, value)` | 设置值（返回 error） |
| `Delete(key)` | 删除键（返回 error） |
| `Keys()` | 获取所有键 |
| `All()` | 获取所有变量为 map |
| `Len()` | 获取变量数量 |
| `GetSecure(key)` | 获取敏感数据的 `SecureValue` |
| `Validate()` | 验证必需键 |
| `ParseInto(&struct)` | 从环境变量填充结构体 |
| `Marshal(data, format?)` | 将 map/struct 转换为格式字符串 |
| `UnmarshalMap(string, format?)` | 解析格式字符串为 map |
| `UnmarshalMapWithConfig(cfg, string, format?)` | 按完整 Config 语义一次性解析（策略/限额/校验） |
| `UnmarshalStruct(string, &struct, format?)` | 解析字符串到结构体 |
| `UnmarshalStructWithConfig(cfg, string, &struct, format?)` | 按完整 Config 语义解析到结构体 |
| `UnmarshalInto(map, &struct)` | 从 map 填充结构体 |
| `MarshalStruct(struct)` | 将结构体转换为 map |
| `IsMarshalError(err)` | 检查是否为序列化错误 |
| `DefaultConfig()` | 安全默认配置 |
| `DevelopmentConfig()` | 宽松限制 + 允许覆盖 |
| `TestingConfig()` | 紧凑限制，隔离测试 |
| `ProductionConfig()` | 严格安全 + 启用审计 |
| `New(cfg...)` | 创建加载器（最多 1 个 Config，传入多个返回 `ErrInvalidConfig`；全零 → `DefaultConfig()`；部分填写必须从 `DefaultConfig()` 出发） |
| `NewSecureValue(string)` | 从字符串创建 SecureValue |
| `NewSecureValueStrict(string)` | 创建 SecureValue，锁定失败时返回错误 |
| `ResetDefaultLoader()` | 重置单例（测试用，返回 error） |
| `ClearBytes([]byte)` | 安全清零字节切片 |
| `ClearInternCache()` | 释放 .env 键驻留缓存（在解析高峰之间调用） |
| `SetMemoryLockEnabled(bool)` | 启用/禁用内存锁定 |
| `IsMemoryLockEnabled()` | 检查内存锁定是否启用 |
| `SetMemoryLockStrict(bool)` | 启用锁定失败的严格模式 |
| `IsMemoryLockStrict()` | 检查严格模式是否启用 |
| `IsMemoryLockSupported()` | 检查平台是否支持内存锁定 |
| `RegisterParser(format, factory)` | 注册自定义解析器 |
| `ForceRegisterParser(format, factory)` | 覆盖内置解析器 |
| `DetectFormat(filename)` | 根据扩展名检测文件格式 |
| `IsSensitiveKey(key)` | 检查键名是否暗示敏感数据 |
| `MaskValue(key, value)` | 根据键敏感性掩码值 |
| `MaskKey(key)` | 掩码键名用于日志 |
| `SanitizeForLog(s)` | 移除字符串中的敏感信息 |
| `MaskSensitiveInString(s)` | 掩码字符串中的敏感内容 |

### Loader 方法

| 方法 | 说明 |
|:-----|:-----|
| **访问** | |
| `GetString(key, def...)` | 获取字符串值 |
| `GetInt(key, def...)` | 获取 `int64` 值 |
| `GetBool(key, def...)` | 获取布尔值 |
| `GetDuration(key, def...)` | 获取时间间隔值 |
| `GetFloat64(key, def...)` | 获取 `float64` 值 |
| `GetUint64(key, def...)` | 获取 `uint64` 值 |
| `Lookup(key)` | 获取值 + 存在性检查 |
| `GetSecure(key)` | 获取敏感数据的 `SecureValue` |
| `Keys()` | 获取所有键 |
| `All()` | 获取所有变量为 map |
| `Len()` | 获取变量数量 |
| **修改** | |
| `Set(key, value)` | 设置值（返回 error） |
| `Delete(key)` | 删除键（返回 error） |
| **文件与生命周期** | |
| `LoadFiles(files...)` | 加载文件到 loader（返回 error） |
| `Apply()` | 应用到 `os.Environ`（返回 error） |
| `ParseInto(&struct)` | 从环境变量填充结构体 |
| `Validate()` | 验证必需键（返回 error） |
| `Close()` | 关闭并清理资源 |
| **状态** | |
| `IsApplied()` | 检查是否已应用到 os.Environ |
| `IsClosed()` | 检查是否已关闭 |
| `LoadTime()` | 获取最后加载时间 |
| `Config()` | 获取 loader 配置（只读） |

---

## 🔐 内存锁定（高级）

对于高安全性应用，启用内存锁定以防止敏感数据被交换到磁盘：

```go
// 在启动时启用内存锁定
env.SetMemoryLockEnabled(true)

// 可选：启用严格模式，锁定失败时报错
env.SetMemoryLockStrict(true)

// 检查平台支持
if env.IsMemoryLockSupported() {
    // 平台支持 mlock/VirtualLock
}

// 创建带锁定的 SecureValue
sv := env.NewSecureValue("api_secret")
defer sv.Release()

// 检查内存是否实际被锁定
if sv.IsMemoryLocked() {
    fmt.Println("内存已保护，不会被交换到磁盘")
}
```

**要求：**
- **Unix**: 需要 `CAP_IPC_LOCK` 权限或 root 权限
- **Windows**: 需要 `SE_LOCK_MEMORY_NAME` 权限

---

## 🔌 自定义解析器（高级）

为其他文件格式注册自定义解析器：

```go
// 注册自定义解析器（不能覆盖内置格式）
err := env.RegisterParser(customFormat, func(cfg env.Config, factory *env.ComponentFactory) (env.EnvParser, error) {
    return &MyCustomParser{validator: factory.Validator()}, nil
})

// 强制覆盖内置解析器（谨慎使用！）
err := env.ForceRegisterParser(env.FormatEnv, customFactory)
```

---

## 🛡️ 安全特性

| 特性 | 描述 |
|:-----|:-----|
| **键/值验证** | 阻止无效格式和危险模式 |
| **禁止键** | 防止覆盖 `PATH`、`LD_PRELOAD`、`DYLD_*` 等 |
| **大小限制** | 文件大小、行长度、变量数量限制 |
| **展开深度** | 防止指数展开攻击 |
| **敏感数据脱敏** | 自动检测并掩码密码、令牌、密钥 |
| **安全内存** | `SecureValue` 在 GC/清理时清零内存 |
| **路径遍历防护** | 阻止 `..`、绝对路径、UNC 路径 |

---

## ⚡ 性能

| 指标 | 数值 |
|:-----|:-----|
| **分片并发** | 32 分片实现并行访问 |
| **内存池** | 可复用的 SecureValue、Builder、Scanner 池 |
| **零分配** | 简单键查找的快速路径 |
| **基准测试** | 运行 `go test -bench=. -benchmem` |

---

## 📁 示例

完整可运行的示例程序请查看 [examples](examples) 目录，每个示例都是独立目录中的自包含程序。

| 示例 | 描述 |
|:-----|:-----|
| [01_quickstart](examples/01_quickstart/) | 全局模式基础：`Load`、类型访问器、`Lookup`、`Set` |
| [02_loader_config](examples/02_loader_config/) | 实例模式、配置预设、前缀过滤、必需键、生命周期 |
| [03_type_access](examples/03_type_access/) | 所有类型 getter：string、int、bool、duration、float、uint、slice（全局 + 实例） |
| [04_struct_mapping](examples/04_struct_mapping/) | `env` 标签结构体序列化/反序列化、嵌套结构体、默认值 |
| [05_secure_values](examples/05_secure_values/) | `SecureValue` 生命周期、`Close` 与 `Release`、JSON 掩码、内存锁定 |
| [06_audit_logging](examples/06_audit_logging/) | JSON、Channel 和 Log 审计处理器、`AuditEvent` 字段 |
| [07_marshal_unmarshal](examples/07_marshal_unmarshal/) | 格式转换：map ↔ string、struct ↔ map、多格式输出、重复键策略 |
| [08_utilities](examples/08_utilities/) | 内省（`Keys`/`All`/`Len`）、脱敏、格式检测 |
| [09_error_handling](examples/09_error_handling/) | 哨兵错误（`errors.Is`）、类型化错误（`errors.As`）、键策略、展开错误 |
| [10_concurrency](examples/10_concurrency/) | 并发读、写和混合访问 |
| [11_advanced](examples/11_advanced/) | 变量展开（含 file-only 作用域）、多文件覆盖、前缀过滤、全局加载器控制 |
| [12_extensibility](examples/12_extensibility/) | 自定义解析器注册（`RegisterParser`）、接口化消费（`EnvGetter`）、自定义组件 |
| [13_customization](examples/13_customization/) | 自定义 `Validator`/`Expander`/`FileSystem`、fail-closed 审计（`ErrAuditFailed`）、自定义 `Marshaler`/`Unmarshaler`、花括号展开运算符 |

示例共享的数据文件位于 [`examples/data/`](examples/data/)。

---

## 📄 许可证

MIT 许可证 - 详见 [LICENSE](LICENSE) 文件。

---

如果这个项目对你有帮助，请给一个 Star! ⭐
