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
**Note**: No test files currently exist in the project. When adding tests, follow Go testing conventions and place them alongside the code files with `_test.go` suffix.

## Architecture and Code Structure

### Core Components
- **main.go**: Entry point that calls `cmd.Execute()`
- **cmd/root.go**: Root command setup, Viper configuration initialization
- **cmd/generate.go**: Main changelog generation logic

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
- `build:`/`deps:` → ⚙️ Dependencies
- `ci:` → 🔄 Continuous Integration
- Breaking changes → 💥 Breaking Changes (separate section)

### Key Dependencies
- **github.com/spf13/cobra**: CLI framework
- **github.com/spf13/viper**: Configuration management
- **github.com/go-git/go-git/v5**: Git operations
- **github.com/Masterminds/semver/v3**: Semantic version parsing
- **github.com/charmbracelet/glamour**: Terminal markdown rendering
- **github.com/sirupsen/logrus**: Structured logging

## Important Implementation Details

1. **Version Tag Sorting**: Tags are sorted using semantic versioning rules, not alphabetically
2. **Commit Deduplication**: Uses a seen map to avoid processing commits multiple times
3. **Error Handling**: Uses `log.Fatalln` for critical errors - consider graceful error handling for library usage
4. **Configuration**: Environment variables must be prefixed with `GOTAGLOG_`
5. **Output**: Detects terminal capabilities for colored/formatted output vs plain markdown

## Release Process

1. Create conventional commits during development
2. Tag with semantic version (e.g., `v1.2.3`)
3. GoReleaser automatically creates multi-platform binaries
4. GitHub Actions run linting on all PRs

## Common Development Tasks

### Adding a New Commit Category
1. Add the category to the `commitCategories` map in cmd/generate.go
2. Add corresponding emoji/label in the same map
3. Update the switch statement in `categorizeCommit()` function

### Modifying Output Format
- Terminal output uses glamour renderer configured in cmd/generate.go
- Markdown output is generated in `generateChangelog()` function
- Breaking changes section is always rendered first when present