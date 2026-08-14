# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

gotaglog is a changelog generator that creates markdown changelogs from git tags based on semantic versioning and conventional commits. It's a CLI tool written in Go using Cobra framework.

## Development Commands

### Building and Running
```bash
# Build the binary
go build

# Run directly
go run main.go

# Install globally
go install

# Update dependencies
go mod tidy
```

### Linting and Code Quality
```bash
# Run golangci-lint (required for PRs)
golangci-lint run

# The project uses godox and revive linters configured in .golangci.yml
```

### Testing
```bash
go test ./...

# Benchmark against a large repository (the case traversal is tuned for)
GOTAGLOG_BENCH_REPO=/path/to/big-repo go test -run=NONE -bench=. -benchmem -count=6 ./cmd/
```

## Architecture and Code Structure

### Core Components
- **main.go**: Entry point that calls `cmd.Execute()`
- **cmd/root.go**: Root command setup, Viper configuration initialization
- **cmd/generate.go**: Main changelog generation logic
- **cmd/repo.go**: Repository opening and commit-graph traversal

### Key Design Patterns
1. **Command Pattern**: Uses Cobra for CLI structure
2. **Configuration Cascade**: Viper handles flags → env vars → config file precedence
3. **Commit Traversal**: Uses Breadth-First Search (BSF) with a seen map to efficiently traverse git history
4. **Conventional Commits**: Categorizes commits by prefix (feat:, fix:, docs:, etc.)
5. **Breaking Changes**: Special handling for commits with `!:` in title or "breaking change:" in message

### Commit Categories Mapping
- `feat:` → ✨ Features
- `fix:` → 🐛 Fixes  
- `docs:` → 📖 Documentation
- `perf:` → ⚡️Performance
- `refactor:` → ✏️ Refactor
- `revert:` → ↩️ Revert
- `style:` → Styling
- `test:` → 🧪 Testing
- `build(deps):` → ⚙️ Dependencies
- `build(deps-dev):` → ⚙️ Dev Dependencies
- `build:` → 🛠️ Build System
- `ci:` → 🔄 Continuous Integration
- `chore:` → Miscellaneous Tasks, except `chore(release):` and `chore(ignore):`, which are dropped
- Breaking changes → 💥 Breaking Changes (separate section)

### Key Dependencies
- **github.com/spf13/cobra**: CLI framework
- **github.com/spf13/viper**: Configuration management
- **github.com/go-git/go-git/v5**: Git operations
- **github.com/Masterminds/semver/v3**: Semantic version parsing
- **github.com/charmbracelet/glamour**: Terminal markdown rendering
- **github.com/sirupsen/logrus**: Structured logging

## Changelog Semantics

A `## [version]` section is `ancestors(tag) - ancestors(previous tag in **semver** order)` — not chronological order, not the branch parent.

A commit appearing in several sections is **correct, not a bug**. A fix released in v1.2.3 on a maintenance line and merged forward is new relative to both v1.2.2 and v2.0.0, so it belongs under v1.2.3 and v2.1.0; collapsing to one section per commit destroys the v2.0.0 → v2.1.0 upgrade path. Check before "fixing": `comm -12 <(git rev-list v2.1.0 ^v2.0.0 | sort) <(git rev-list v1.2.3 | sort)`.

Corollary: inconsistent tag naming makes sections misleading — `v2.0.0-beta9` sorts *above* `v2.0.0-beta.13`, so its predecessor is chronologically later.

## Important Implementation Details

1. **Version Tag Sorting**: Tags are sorted using semantic versioning rules, not alphabetically
2. **Commit Deduplication**: Uses a seen map to avoid processing commits multiple times
3. **Error Handling**: Uses `log.Fatalln` for critical errors - consider graceful error handling for library usage
4. **Configuration**: Environment variables must be prefixed with `GOTAGLOG_`
5. **Output**: Detects terminal capabilities for colored/formatted output vs plain markdown
6. **Repository access**: use `openRepository` (cmd/repo.go), never `git.PlainOpen` directly — the default storage reopens the packfile on every object read

## Release Process

1. Create conventional commits during development
2. Tag with semantic version (e.g., `v1.2.3`)
3. GoReleaser automatically creates multi-platform binaries
4. GitHub Actions run linting on all PRs

## Common Development Tasks

### Adding a New Commit Category
Add a `matchGroup(pattern, label)` or `skipGroup(pattern)` entry to the `commitGroups` slice in cmd/generate.go. First match wins, so `^build\(deps\)` must stay above `^build`.

### Modifying Output Format
- Terminal output uses glamour renderer configured in cmd/generate.go
- Markdown output is generated in `generateChangelog()` function
- Breaking changes section is always rendered first when present