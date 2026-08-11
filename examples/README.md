# Examples

Runnable examples for the `env` library. Each example is a self-contained
program in its own directory.

## Running

```powershell
# Run from the project root so relative data paths resolve correctly.
go run -tags examples ./examples/01_quickstart/
```

All examples use the `//go:build examples` tag so they are excluded from
normal builds (`go build ./...`).

## Example Index

| # | Directory | Topic |
|---|-----------|-------|
| 01 | `01_quickstart/` | Global mode basics: Load, GetString/Int/Bool/Duration, Lookup, Set |
| 02 | `02_loader_config/` | Instance mode, config presets (Default/Dev/Prod/Testing), prefix, required keys, lifecycle |
| 03 | `03_type_access/` | All typed getters: string, int, bool, duration, float, uint, slice, lookup |
| 04 | `04_struct_mapping/` | Struct unmarshal/marshal with `env` tags, nested structs, defaults |
| 05 | `05_secure_values/` | SecureValue lifecycle, Close vs Release, memory locking |
| 06 | `06_audit_logging/` | JSON, Channel, and Log audit handlers |
| 07 | `07_marshal_unmarshal/` | Format conversion: map ↔ string, struct ↔ map, multi-format output |
| 08 | `08_utilities/` | Introspection (Keys/All/Len), masking, format detection |
| 09 | `09_error_handling/` | Sentinel errors (errors.Is), typed errors (errors.As) |
| 10 | `10_concurrency/` | Concurrent reads, writes, and mixed access |
| 11 | `11_advanced/` | Variable expansion, multiple file override, prefix filter |

## Data Files

Shared configuration files live in `examples/data/`:

| File | Format | Purpose |
|------|--------|---------|
| `config.env` | dotenv | Flat KEY=value pairs, variable expansion demo |
| `config.json` | JSON | Nested structure (app/db/cache/features) |
| `config.yaml` | YAML | Same structure as JSON, for struct mapping demos |
