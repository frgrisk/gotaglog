# gotaglog

[![Go Report Card](https://goreportcard.com/badge/github.com/frgrisk/gotaglog)](https://goreportcard.com/report/github.com/frgrisk/gotaglog)

GoTagLog is a simple command-line application to automatically generate a
changelog from git tags based on the semantic versioning. It categorizes
commits into groups and outputs them in a formatted markdown file or
directly to the terminal with highlighting.

## Prerequisites

To use GoTagLog, you'll need to have:

- [Go](https://golang.org/dl/) installed on your local machine.

## Installation

```bash
go install github.com/frgrisk/gotaglog@latest
```

## Usage

To generate a changelog, use the following command:

```bash
gotaglog
```

### Flags

The application accepts several flags:

- `--config`: path to configuration file (default is `$HOME/.gotaglog.yaml`).
- `-o, --output`: path to output file (default if to print to stdout).
- `-r, --repo`: repo to generate changelog for (default is current directory).
- `-t, --tag`: semantic version tag for unreleased changes (default is
  "unreleased").
- `--inc-patch`: increment patch version (default is false). Takes
  precedence over `--tag`.
- `--inc-minor`: increment patch version (default is false). Takes
  precedence over `--inc-patch` and `--tag`.
- `--inc-major`: increment patch version (default is false). Takes
  precedence over `--inc-minor`, `--inc-patch`, and `--tag`.
- `--unreleased`: show only unreleased changes.

### Environment variables

In addition to flags and the configuration file, you can also use
environment variables to set parameters. The application will automatically
look for any environment variables beginning with `GOTAGLOG_`. For
instance, to set the repo, you could use the following command:

```bash
export GOTAGLOG_REPO=/path/to/repo
```

## License

GoTagLog is released under the MIT License. See the [LICENSE](./LICENSE)
file for more details.

## Acknowledgments

- Inspired by [git-cliff](https://github.com/orhun/git-cliff)
- Built with [go-git](https://github.com/go-git/go-git)
- Command line interface powered by [cobra](https://github.com/spf13/cobra)
- Configuration management using [viper](https://github.com/spf13/viper)
- Markdown rendering by [glamour](https://github.com/charmbracelet/glamour)
