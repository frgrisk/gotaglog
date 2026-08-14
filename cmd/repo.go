package cmd

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	log "github.com/sirupsen/logrus"
)

// openRepository opens the repository at path with its packfile handle and its
// packfile and loose-object listings cached, and returns a func that releases
// them. Both caches assume nothing else writes the repository while the command
// runs; if that stops holding, reads see a stale object listing. Without them
// go-git reopens the packfile for every object read, which costs more than the
// decoding does.
//
// PlainOpen resolves ".git" — directory, gitdir file, or bare repository — so
// that logic is not duplicated here. The worktree is not needed: nothing in the
// changelog is read from it.
func openRepository(path string) (*git.Repository, func(), error) {
	base, err := git.PlainOpen(path)
	if err != nil {
		return nil, nil, err
	}

	dotGit, ok := base.Storer.(*filesystem.Storage)
	if !ok {
		log.Warn("Unrecognized git storage; falling back to untuned object access")
		return base, func() {}, nil
	}

	storer := filesystem.NewStorageWithOptions(
		dotGit.Filesystem(),
		cache.NewObjectLRUDefault(),
		filesystem.Options{ExclusiveAccess: true, KeepDescriptors: true},
	)
	repo, err := git.Open(storer, nil)
	if err != nil {
		return nil, nil, err
	}

	return repo, func() {
		// Nothing was written and the only descriptors are read-only packfile
		// handles, so a close failure cannot lose data.
		if err := storer.Close(); err != nil {
			log.Debugf("Cannot close repository storage: %v", err)
		}
	}, nil
}

// commitGraph caches what a run reads more than once: the parent edges of every
// commit it walks, and the commit each tag resolves to. The tag loop walks the
// full ancestry of every tag, so a commit near the root is visited once per tag;
// caching edges rather than commits keeps the retained memory proportional to
// the number of commits rather than to their combined message size.
type commitGraph struct {
	repo    *git.Repository
	parents map[plumbing.Hash][]plumbing.Hash
	tags    map[plumbing.Hash]*object.Commit

	// header is reused across parentsOf calls; a fresh bufio.Reader per commit
	// would allocate a 4KB buffer for the ~100 bytes it actually reads.
	header *bufio.Reader
}

func newCommitGraph(repo *git.Repository) *commitGraph {
	return &commitGraph{
		repo:    repo,
		parents: make(map[plumbing.Hash][]plumbing.Hash),
		tags:    make(map[plumbing.Hash]*object.Commit),
		header:  bufio.NewReader(nil),
	}
}

// parentsOf returns h's parent hashes, reading h at most once per run.
//
// It parses the commit header instead of calling repo.CommitObject because a
// full decode materializes the commit message, which is most of the object's
// bytes and is never used for a commit the changelog does not list.
func (g *commitGraph) parentsOf(h plumbing.Hash) (parents []plumbing.Hash, err error) {
	if p, ok := g.parents[h]; ok {
		return p, nil
	}

	obj, err := g.repo.Storer.EncodedObject(plumbing.CommitObject, h)
	if err != nil {
		return nil, err
	}
	r, err := obj.Reader()
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := r.Close(); err == nil {
			err = cerr
		}
	}()

	g.header.Reset(r)
	for {
		line, readErr := g.header.ReadSlice('\n')
		// A tree line is 45 bytes and a parent line 47, so a line too long for
		// the buffer is a later header and ends the parent list.
		if readErr != nil && readErr != io.EOF && readErr != bufio.ErrBufferFull {
			return nil, fmt.Errorf("reading header of commit %s: %w", h, readErr)
		}
		line = bytes.TrimSuffix(line, []byte("\n"))

		raw, isParent := bytes.CutPrefix(line, []byte("parent "))
		if !isParent {
			// Commit headers are ordered tree, parent, author, so the only line
			// that can precede the parents is the tree.
			if !bytes.HasPrefix(line, []byte("tree ")) || readErr != nil {
				break
			}
			continue
		}

		var parent plumbing.Hash
		if len(raw) != hex.EncodedLen(len(parent)) {
			return nil, fmt.Errorf("commit %s: malformed parent %q", h, raw)
		}
		if _, err := hex.Decode(parent[:], raw); err != nil {
			return nil, fmt.Errorf("commit %s: malformed parent %q: %w", h, raw, err)
		}
		parents = append(parents, parent)

		if readErr != nil {
			break
		}
	}

	g.parents[h] = parents
	return parents, nil
}

// walk visits start and its ancestors, skipping any commit in skip (which it
// does not modify), and returns the set it visited. hint sizes the visited set
// when the caller knows roughly how large it will be.
//
// The order reproduces object.NewCommitIterBSF and is load-bearing: the
// changelog lists commits in the order they are walked. TestWalkMatchesBSF
// pins the two together.
func (g *commitGraph) walk(
	start plumbing.Hash,
	skip map[plumbing.Hash]bool,
	hint int,
	visit func(plumbing.Hash) error,
) (map[plumbing.Hash]bool, error) {
	seen := make(map[plumbing.Hash]bool, hint)
	if skip[start] {
		return seen, nil
	}

	seen[start] = true
	queue := make([]plumbing.Hash, 1, max(hint, 16))
	queue[0] = start

	for i := 0; i < len(queue); i++ {
		parents, err := g.parentsOf(queue[i])
		if err != nil {
			return nil, err
		}
		for _, p := range parents {
			if seen[p] || skip[p] {
				continue
			}
			seen[p] = true
			queue = append(queue, p)
		}

		if err := visit(queue[i]); err != nil {
			return nil, err
		}
	}

	return seen, nil
}

// ancestors returns every commit reachable from h, h included. len(g.parents)
// is an upper bound on the result once HEAD has been walked.
func (g *commitGraph) ancestors(h plumbing.Hash) (map[plumbing.Hash]bool, error) {
	return g.walk(h, nil, len(g.parents), func(plumbing.Hash) error { return nil })
}

// tagCommit resolves tag to the commit it names, reading each tag at most once.
// An annotated tag costs two object reads, and the loop resolves every tag
// around four times.
func (g *commitGraph) tagCommit(tag *plumbing.Reference) *object.Commit {
	if c, ok := g.tags[tag.Hash()]; ok {
		return c
	}

	var commit *object.Commit
	obj, err := g.repo.TagObject(tag.Hash())
	if err != nil {
		// Failing to read the ref as a tag object means it is a lightweight
		// tag, which points straight at a commit.
		commit, err = g.repo.CommitObject(tag.Hash())
		if err != nil {
			log.Fatalln("Cannot retrieve commit from tag:", err)
		}
	} else {
		commit, err = obj.Commit()
		if err != nil {
			log.Fatalln("Cannot retrieve commit from tag object:", err)
		}
	}

	g.tags[tag.Hash()] = commit
	return commit
}
