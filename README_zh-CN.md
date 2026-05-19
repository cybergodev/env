# Env - Go 高性能环境变量库

[![Go Reference](https://pkg.go.dev/badge/github.com/cybergodev/env.svg)](https://pkg.go.dev/github.com/cybergodev/env)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Security Policy](https://img.shields.io/badge/security-policy-blue.svg)](docs/SECURITY.md)
[![Thread Safe](https://img.shields.io/badge/thread%20safe-%E2%9C%93-brightgreen.svg)](docs/CONCURRENCY_SAFETY.md)

**[English Documentation](README.md)** | **[www.cybergo.dev/env](https://www.cybergo.dev/env)**

---

## 📋 概述

**Env** 是一个生产级零依赖、线程安全的 Go 环境变量管理库，专注于**安全性**、**并发性**和**开发者体验**。

### ✨ 核心特性

| 特性 | 描述 |
|:-----|:-----|
| 🚀 **一行配置** | `env.Load(".env")` 即可加载并应用到 `os.Environ` |
| 🔒 **类型安全** | `GetString`、`GetInt`、`GetBool`、`GetDuration`、`GetSlice[T]` |
| 📁 **多格式支持** | 自动检测 `.env`、`.json`、`.yaml` 文件 |
| ⚡ **线程安全** | 分片存储（8 分片）+ RWMutex，高并发优化 |
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
// 多文件初始化（后加载的覆盖先加载的）
env.Load(".env", "config.json", ".env.local")

// 检查存在性
value, exists := env.Lookup("KEY")
if !exists {
    // 处理缺失的键
}

// CRUD 操作
env.Set("KEY", "value")           // 设置值（返回 error）
env.Delete("KEY")                 // 删除键（返回 error）
keys := env.Keys()                // 获取所有键
all := env.All()                  // 获取所有变量为 map
count := env.Len()                // 变量数量
```

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
```

**解析策略：**

| 步骤 | 简单键（无点号） | 点号语法键 |
|:-----|:----------------|:-----------|
| 1 | 精确匹配 | 点号转下划线，转大写 → 查找 |
| 2 | 大写回退 | （已在步骤 1 完成） |

**最佳实践：** 在 `.env` 文件中使用 `UPPER_SNAKE_CASE`（大写下划线格式），确保所有查找方式均可正确工作。

### 结构体映射

```go
type Config struct {
    Port    int           `env:"PORT" envDefault:"8080"`
    Debug   bool          `env:"DEBUG" envDefault:"false"`
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
cfg.Filenames = []string{"/etc/app/.env"}

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

---

## 🔄 变量展开

`.env` 文件完整支持 `${VAR}` 语法：

```env
HOST=localhost
PORT=8080

# 变量引用
URL=${HOST}:${PORT}                    # → "localhost:8080"

# 未设置或为空时使用默认值
TIMEOUT=${TIMEOUT:-30s}

# 仅未设置时使用默认值（保留空字符串）
NAME=${NAME-default}

# 组合展开
FULL_URL=https://${HOST}:${PORT:-443}
```

---

## 🔒 安全值处理

使用 `SecureValue` 处理密码、API 密钥、令牌等敏感数据：

```go
// 获取 SecureValue
sv := env.GetSecure("API_KEY")
if sv != nil {
    defer sv.Release()

    // 安全日志输出（String() 返回掩码表示）
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
cfg := env.ProductionConfig()
cfg.AuditEnabled = true
cfg.AuditHandler = env.NewJSONAuditHandler(os.Stdout)

loader, _ := env.New(cfg)
// 输出: {"action":"set","key":"API_KEY","success":true,"timestamp":"..."}
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

// 测试之间重置默认加载器
func TestMain(m *testing.M) {
    env.ResetDefaultLoader()
    os.Exit(m.Run())
}
```

---

## 🛠️ 工具函数

```go
// 敏感键检测
env.IsSensitiveKey("API_SECRET")  // true
env.IsSensitiveKey("HOST")        // false

// 值掩码
env.MaskValue("API_KEY", "secret123")  // "***"

// 键掩码用于日志
env.MaskKey("DB_PASSWORD")  // "DB_***"

// 日志安全处理
safe := env.SanitizeForLog(userInput)

// 掩码字符串中的敏感内容
masked := env.MaskSensitiveInString(logMessage)

// 格式检测
env.DetectFormat("config.yaml")  // FormatYAML
```

### 敏感键模式

自动检测的模式（不区分大小写）：

```
*password*, *secret*, *key*, *token*, *credential*,
*api_key*, *private*, *auth*, *session*, *access*
```

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
| `MaxFileSize` | 2 MB | 10 MB | 64 KB | 64 KB |
| `MaxVariables` | 500 | 500 | 50 | 50 |

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

// === 安全限制（LimitsConfig）===
cfg.MaxFileSize       = 2 << 20   // 2 MB
cfg.MaxVariables      = 500
cfg.MaxLineLength     = 1024
cfg.MaxKeyLength      = 64
cfg.MaxValueLength    = 4096
cfg.MaxExpansionDepth = 5

// === 解析（ParsingConfig）===
cfg.AllowExportPrefix = true    // 允许 "export KEY=value" 语法
cfg.AllowYamlSyntax   = false   // .env 中的 YAML 风格值
cfg.ExpandVariables   = true    // 展开 ${VAR} 引用

// === JSON 选项（JSONConfig）===
cfg.JSONNullAsEmpty    = true
cfg.JSONNumberAsString = true
cfg.JSONBoolAsString   = true
cfg.JSONMaxDepth       = 10

// === YAML 选项（YAMLConfig）===
cfg.YAMLNullAsEmpty    = true
cfg.YAMLNumberAsString = true
cfg.YAMLBoolAsString   = true
cfg.YAMLMaxDepth       = 10

// === 高级选项（ComponentConfig）===
cfg.Prefix     = "APP_"      // 仅加载带前缀的键
cfg.FileSystem = nil         // nil = 操作系统文件系统

// === 审计日志（ComponentConfig）===
cfg.AuditEnabled = true
cfg.AuditHandler = env.NewJSONAuditHandler(os.Stdout)
```

> **注意：** Config 使用嵌套结构体（`FileConfig`、`ValidationConfig`、`LimitsConfig`、
> `JSONConfig`、`YAMLConfig`、`ParsingConfig`、`ComponentConfig`）并通过字段提升访问。
> 你可以直接访问字段（`cfg.Filenames`）或显式访问（`cfg.FileConfig.Filenames`）。

### 默认限制

| 设置 | 默认值 | 硬限制 |
|------|--------|--------|
| MaxFileSize | 2 MB | 100 MB |
| MaxLineLength | 1,024 字符 | 64 KB |
| MaxKeyLength | 64 字符 | 1,024 字符 |
| MaxValueLength | 4,096 字符 | 1 MB |
| MaxVariables | 500 | 10,000 |
| MaxExpansionDepth | 5 | 20 |

---

## 📖 API 参考

### 包函数

| 函数 | 说明 |
|:-----|:-----|
| `Load(files...)` | 加载文件并应用到 `os.Environ` |
| `LoadWithConfig(cfg)` | 使用自定义配置加载 |
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
| `UnmarshalStruct(string, &struct, format?)` | 解析字符串到结构体 |
| `UnmarshalInto(map, &struct)` | 从 map 填充结构体 |
| `MarshalStruct(struct)` | 将结构体转换为 map |
| `IsMarshalError(err)` | 检查是否为序列化错误 |
| `New(cfg)` | 使用配置创建新加载器 |
| `NewSecureValue(string)` | 从字符串创建 SecureValue |
| `NewSecureValueStrict(string)` | 创建 SecureValue，锁定失败时返回错误 |
| `ResetDefaultLoader()` | 重置单例（测试用，返回 error） |
| `ClearBytes([]byte)` | 安全清零字节切片 |
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
| **分片并发** | 8 分片实现并行访问 |
| **内存池** | 可复用的 SecureValue、Builder、Scanner 池 |
| **零分配** | 简单键查找的快速路径 |
| **基准测试** | 运行 `go test -bench=. -benchmem` |

---

## 📁 示例

完整示例代码请查看 [examples](examples) 目录：

| 示例 | 描述 |
|:-----|:-----|
| [01_quickstart.go](examples/01_quickstart.go) | 基础用法 |
| [02_loader_config.go](examples/02_loader_config.go) | 配置选项 |
| [03_type_access.go](examples/03_type_access.go) | 类型转换 |
| [04_struct_mapping.go](examples/04_struct_mapping.go) | 结构体填充 |
| [05_secure_values.go](examples/05_secure_values.go) | 安全处理 |
| [06_audit_logging.go](examples/06_audit_logging.go) | 审计日志 |
| [07_marshal_unmarshal.go](examples/07_marshal_unmarshal.go) | 序列化 |
| [08_utilities.go](examples/08_utilities.go) | 工具函数 |

---

## 📄 许可证

MIT 许可证 - 详见 [LICENSE](LICENSE) 文件。

---

如果这个项目对你有帮助，请给一个 Star! ⭐
