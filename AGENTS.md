# Repository Guidelines

## Project Structure & Module Organization

This is a Go module for a LAN device monitor. The CLI entry point is in `cmd/local-monitor/main.go`. Internal packages live under `internal/`: `config` loads YAML settings, `database` manages SQLite persistence, and `monitor` performs ARP probing. `config.example.yaml` documents runtime configuration, while `README.md` covers user-facing usage. Tests should be colocated with the package they exercise as `*_test.go` files.

## Build, Test, and Development Commands

- `go test ./...`: run all package tests.
- `go test ./internal/monitor`: run tests for one package while iterating.
- `go build -o local-monitor ./cmd/local-monitor`: build the CLI binary.
- `CGO_ENABLED=0 go build -o local-monitor ./cmd/local-monitor`: build a pure-Go binary using `modernc.org/sqlite`.
- `go run ./cmd/local-monitor -config config.yaml -status`: run locally and print current statuses.
- `go run ./cmd/local-monitor -config config.yaml -probe -json`: perform one ARP probe and emit JSON.

ARP probing requires raw socket privileges, so probe and monitor modes may need Administrator/root access. Status and cleanup commands only need database access.

## Coding Style & Naming Conventions

Use standard Go formatting: run `gofmt` on changed `.go` files before committing. Keep package names short, lowercase, and aligned with directory names. Exported identifiers should have clear doc-friendly names; unexported helpers should stay scoped to the package. Prefer structured errors with context, for example `fmt.Errorf("device %s: %w", name, err)`. YAML field names use snake case, matching `config.example.yaml` and struct tags such as `retry_count`.

## Testing Guidelines

Use Go's built-in `testing` package unless a stronger project need appears. Name tests `TestXxx` and table-driven cases with descriptive `name` fields. Keep tests close to the package under test, for example `internal/config/config_test.go`. Avoid tests that require live network devices by default; isolate ARP behavior behind small units or use fakes where practical. Run `go test ./...` before opening a pull request.

## Commit & Pull Request Guidelines

No established commit-message pattern is visible in this checkout, so use concise imperative subjects such as `Add config validation` or `Fix status JSON output`. Keep each commit focused on one logical change.

Pull requests should include a short summary, the motivation or linked issue, testing performed, and any configuration or privilege implications. Include CLI output examples when behavior changes, especially for `-status`, `-probe`, or `-json`.

## Security & Configuration Tips

Do not commit real `config.yaml` files containing private device names, IPs, or MAC addresses. Use `config.example.yaml` for documentation updates. Treat generated SQLite databases such as `local-monitor.db` as local runtime artifacts, not source files.
