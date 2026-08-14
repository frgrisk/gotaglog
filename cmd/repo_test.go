package cmd

import (
	"slices"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// TestWalkMatchesBSF pins commitGraph.walk to the go-git iterator it
// deliberately reproduces. The changelog lists commits in walk order, and the
// two only agree because both check the visited set at enqueue and at dequeue;
// if a go-git upgrade changes that, sections silently reorder.
func TestWalkMatchesBSF(t *testing.T) {
	repo, closeRepo, err := openRepository("..")
	if err != nil {
		t.Skipf("cannot open repository: %v", err)
	}
	defer closeRepo()

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("cannot resolve HEAD: %v", err)
	}
	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("cannot read HEAD commit: %v", err)
	}

	var want []plumbing.Hash
	err = object.NewCommitIterBSF(headCommit, nil, nil).ForEach(func(c *object.Commit) error {
		want = append(want, c.Hash)
		return nil
	})
	if err != nil {
		t.Fatalf("cannot walk history with go-git: %v", err)
	}

	var got []plumbing.Hash
	if _, err := newCommitGraph(repo).walk(head.Hash(), nil, 0, func(h plumbing.Hash) error {
		got = append(got, h)
		return nil
	}); err != nil {
		t.Fatalf("cannot walk history: %v", err)
	}

	if !slices.Equal(got, want) {
		t.Fatalf("walk order drifted from object.NewCommitIterBSF: visited %d commits, want %d", len(got), len(want))
	}
	if len(want) == 0 {
		t.Fatal("walked no commits, so the comparison proved nothing")
	}
}

// TestPlainOpenUsesFilesystemStorage pins the assumption openRepository is
// built on. If PlainOpen stops returning a *filesystem.Storage, openRepository
// falls back to untuned object access and the tool silently gets slow again.
func TestPlainOpenUsesFilesystemStorage(t *testing.T) {
	repo, err := git.PlainOpen("..")
	if err != nil {
		t.Skipf("cannot open repository: %v", err)
	}
	if _, ok := repo.Storer.(*filesystem.Storage); !ok {
		t.Fatalf("PlainOpen returned %T, want *filesystem.Storage", repo.Storer)
	}
}
