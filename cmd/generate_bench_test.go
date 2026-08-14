package cmd

import (
	"cmp"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/spf13/viper"
)

// benchRepo is the repository the changelog benchmarks run against. It defaults
// to this repository, which is small; point GOTAGLOG_BENCH_REPO at a repository
// with many tags and deep history to measure the case the traversal is tuned
// for.
func benchRepo(b *testing.B) string {
	b.Helper()

	repo := cmp.Or(os.Getenv("GOTAGLOG_BENCH_REPO"), "..")
	if _, err := git.PlainOpen(repo); err != nil {
		b.Skipf("bench repo %q unavailable: %v", repo, err)
	}
	return repo
}

func BenchmarkGetChangeLog(b *testing.B) {
	repo := benchRepo(b)
	out := filepath.Join(b.TempDir(), "CHANGELOG.md")

	viper.Set("repo", repo)
	viper.Set("output", out)
	viper.Set("tag", defaultUnreleasedTag)

	b.ReportAllocs()
	for b.Loop() {
		getChangeLog()
	}
}
