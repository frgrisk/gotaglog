package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"github.com/charmbracelet/glamour"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/term"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type CommitGroup struct {
	Message string
	Group   string
	Skip    bool
}

var commitGroups = []CommitGroup{
	{Message: "^feat", Group: "✨ Features"},
	{Message: "^fix", Group: "🐛 Fixes"},
	{Message: "^docs", Group: "📖 Documentation"},
	{Message: "^perf", Group: "⚡️Performance"},
	{Message: "^refactor", Group: "✏️ Refactor"},
	{Message: "^revert", Group: "↩️ Revert"},
	{Message: "^style", Group: "Styling"},
	{Message: "^test", Group: "🧪 Testing"},
	{Message: "^build\\(deps\\)", Group: "⚙️ Dependencies"},
	{Message: "^build\\(deps-dev\\)", Group: "⚙️ Dev Dependencies"},
	{Message: "^build", Group: "🛠️ Build System"},
	{Message: "^ci", Group: "🔄 Continuous Integration"},
	{Message: "^chore\\(release\\)", Skip: true},
	{Message: "^chore\\(ignore\\)", Skip: true},
	{Message: "^chore", Group: "Miscellaneous Tasks"},
}

func getChangeLog() {
	repoPath := viper.GetString("repo")
	if repoPath == "" {
		log.Fatalln("Repository path is empty")
		return
	}
	repoPath = filepath.Clean(repoPath)
	log.Debugf("Repository path is set to %q", repoPath)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		log.Fatalln("Cannot open repository:", err)
		return
	}

	tags, err := repo.Tags()
	if err != nil {
		log.Fatalln("Cannot fetch tags:", err)
		return
	}

	var semverTags semver.Collection
	tagMap := make(map[string]*plumbing.Reference)

	err = tags.ForEach(func(tag *plumbing.Reference) error {
		ver, err := semver.NewVersion(tag.Name().Short())
		if err == nil {
			semverTags = append(semverTags, ver)
			tagMap[ver.String()] = tag
		}
		return nil
	})
	if err != nil {
		log.Fatalln("Cannot iterate tags:", err)
		return
	}

	sort.Sort(semverTags)

	var prevTag *plumbing.Reference

	// keep track of already processed commits to avoid re-traversing them
	seen := make(map[plumbing.Hash]bool)

	var changelog []string

	// Find the most recent tag that is an ancestor of HEAD
	head, err := repo.Head()
	if err != nil {
		log.Fatalln("Cannot resolve HEAD:", err)
	}
	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		log.Fatalln("Cannot fetch HEAD commit:", err)
	}
	
	// Filter tags to only include those that are ancestors of HEAD
	// This ensures we don't include tags from other branches when generating
	// a changelog from a specific branch (e.g., v0.10 branch shouldn't include
	// tags from v25.x branch)
	var ancestorTags semver.Collection
	ancestorTagMap := make(map[string]*plumbing.Reference)
	
	for _, ver := range semverTags {
		tag := tagMap[ver.String()]
		tagCommit := getTagCommit(repo, tag)
		
		// Check if this tag is an ancestor of HEAD
		isAncestor, err := isAncestorCommit(repo, tagCommit, headCommit)
		if err != nil {
			log.Warnf("Error checking ancestry for tag %s: %v", ver.String(), err)
			continue
		}
		if isAncestor {
			ancestorTags = append(ancestorTags, ver)
			ancestorTagMap[ver.String()] = tag
		}
	}
	
	// Re-sort the filtered tags
	sort.Sort(ancestorTags)
	
	var lastAncestorTag *plumbing.Reference
	var lastAncestorVer *semver.Version
	if len(ancestorTags) > 0 {
		lastAncestorVer = ancestorTags[len(ancestorTags)-1]
		lastAncestorTag = ancestorTagMap[lastAncestorVer.String()]
	}

	// If --unreleased flag is set, only generate unreleased changes
	if viper.GetBool("unreleased") && lastAncestorTag != nil {
		unreleasedSeen := make(map[plumbing.Hash]bool)
		entry := getTagEntryDetails(repo, lastAncestorTag, nil, unreleasedSeen)
		if entry != "" {
			unreleasedTag := viper.GetString("tag")
			unreleasedHeader := fmt.Sprintf("## [%s]", unreleasedTag)
			if viper.GetBool("inc-major") {
				unreleasedVer := lastAncestorVer.IncMajor()
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", &unreleasedVer, time.Now().Format("2006-01-02"))
			} else if viper.GetBool("inc-minor") {
				unreleasedVer := lastAncestorVer.IncMinor()
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", &unreleasedVer, time.Now().Format("2006-01-02"))
			} else if viper.GetBool("inc-patch") {
				unreleasedVer := lastAncestorVer.IncPatch()
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", &unreleasedVer, time.Now().Format("2006-01-02"))
			} else if unreleasedTag != defaultUnreleasedTag {
				unreleasedVer, err := semver.NewVersion(unreleasedTag)
				if err != nil {
					log.WithField("tag", unreleasedTag).Fatal(err)
				}
				if unreleasedVer.LessThan(lastAncestorVer) {
					log.Warnf("Unreleased tag %q is lower than existing tag %q in the repository.", unreleasedVer, lastAncestorVer)
				}
				if unreleasedVer.Equal(lastAncestorVer) {
					log.Warnf("Unreleased tag %q already exists in the repository.", unreleasedVer)
				}
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", unreleasedVer, time.Now().Format("2006-01-02"))
			}
			changelog = []string{"# Changelog\n", unreleasedHeader, entry}
		} else {
			changelog = []string{"# Changelog\n"}
		}
	} else {
		// Regular changelog generation
		for _, ver := range ancestorTags {
			tag := ancestorTagMap[ver.String()]
			entry := fmt.Sprintf("## [%s] - %s\n", ver.String(), getTagCommit(repo, tag).Author.When.Format("2006-01-02"))
			entry += getTagEntryDetails(repo, prevTag, tag, seen)
			changelog = append([]string{entry}, changelog...)
			prevTag = tag
			if lastAncestorTag != nil && ver == lastAncestorVer {
				// For unreleased changes, use a fresh seen map to avoid excluding
				// commits that were processed in other branches/tags
				unreleasedSeen := make(map[plumbing.Hash]bool)
				entry = getTagEntryDetails(repo, tag, nil, unreleasedSeen)
				unreleasedTag := viper.GetString("tag")
			unreleasedHeader := fmt.Sprintf("## [%s]", unreleasedTag)
			if viper.GetBool("inc-major") {
				unreleasedVer := ver.IncMajor()
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", &unreleasedVer, time.Now().Format("2006-01-02"))
			} else if viper.GetBool("inc-minor") {
				unreleasedVer := ver.IncMinor()
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", &unreleasedVer, time.Now().Format("2006-01-02"))
			} else if viper.GetBool("inc-patch") {
				unreleasedVer := ver.IncPatch()
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", &unreleasedVer, time.Now().Format("2006-01-02"))
			} else if unreleasedTag != defaultUnreleasedTag {
				unreleasedVer, err := semver.NewVersion(unreleasedTag)
				if err != nil {
					log.WithField("tag", unreleasedTag).Fatal(err)
				}
				if unreleasedVer.LessThan(ver) {
					log.Warnf("Unreleased tag %q is lower than existing tag %q in the repository.", unreleasedVer, ver)
				}
				if unreleasedVer.Equal(ver) {
					log.Warnf("Unreleased tag %q already exists in the repository.", unreleasedVer)
				}
				unreleasedHeader = fmt.Sprintf("## [%s] - %s", unreleasedVer, time.Now().Format("2006-01-02"))
			}
			unreleasedEntry := []string{unreleasedHeader, entry}
			if entry != "" {
				changelog = append(unreleasedEntry, changelog...)
			}
		}
	}
		changelog = append([]string{"# Changelog\n"}, changelog...)
	}
	if viper.GetString("output") != "" {
		err = os.WriteFile(viper.GetString("output"), []byte(strings.Join(changelog, "\n")), 0644)
		if err != nil {
			log.Fatalln("Cannot write to file:", err)
		}
		return
	}

	// initialize glamour
	isTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	style := "auto"
	// We want to use a special no-TTY style, when stdout is not a terminal
	// and there was no specific style passed by arg
	if !isTerminal {
		style = "notty"
	}

	// Detect terminal width
	var width uint
	if isTerminal {
		w, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err == nil {
			width = uint(w)
		}

		if width > 120 {
			width = 120
		}
	}
	if width == 0 {
		width = 80
	}

	// initialize glamour
	var gs glamour.TermRendererOption
	if style == "auto" {
		gs = glamour.WithEnvironmentConfig()
	} else {
		gs = glamour.WithStylePath(style)
	}
	r, err := glamour.NewTermRenderer(
		gs,
		glamour.WithWordWrap(int(width)),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		log.Fatalln("Cannot create terminal renderer:", err)
	}

	out, err := r.Render(strings.Join(changelog, "\n"))
	if err != nil {
		log.Fatalln("Cannot render changelog:", err)
	}
	fmt.Print(out)
}

// getCommitsInRange returns commits that are reachable from newerTag but not from olderTag
func getCommitsInRange(repo *git.Repository, olderTag, newerTag *plumbing.Reference) ([]*object.Commit, error) {
	var until *object.Commit
	var err error

	if newerTag != nil {
		until = getTagCommit(repo, newerTag)
	} else {
		head, err := repo.Head()
		if err != nil {
			return nil, err
		}
		until, err = repo.CommitObject(head.Hash())
		if err != nil {
			return nil, err
		}
	}

	// Get all commits reachable from olderTag (if any)
	olderCommits := make(map[plumbing.Hash]bool)
	if olderTag != nil {
		olderCommit := getTagCommit(repo, olderTag)
		olderCommits[olderCommit.Hash] = true
		olderIter := object.NewCommitIterBSF(olderCommit, nil, nil)
		err = olderIter.ForEach(func(c *object.Commit) error {
			olderCommits[c.Hash] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Get commits reachable from until that are not in olderCommits
	var commits []*object.Commit
	untilIter := object.NewCommitIterBSF(until, nil, nil)
	err = untilIter.ForEach(func(c *object.Commit) error {
		if !olderCommits[c.Hash] {
			commits = append(commits, c)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return commits, nil
}

func getTagEntryDetails(repo *git.Repository, olderTag, newerTag *plumbing.Reference, _ map[plumbing.Hash]bool) string {
	// Get commits that are in this specific tag range
	commits, err := getCommitsInRange(repo, olderTag, newerTag)
	if err != nil {
		log.Fatalln("Cannot get commits in range:", err)
	}

	var entry string

	groupedCommits := make(map[string][]string)
	var breakingChanges []string

	for _, c := range commits {
		// Only print the first line of the commit message (the title)
		title := strings.Split(c.Message, "\n")[0]
		isBreaking := strings.Contains(title, "!:") ||
			strings.Contains(strings.ToLower(c.Message), "breaking change:") ||
			strings.Contains(strings.ToLower(c.Message), "breaking-change:")

		for _, group := range commitGroups {
			re := regexp.MustCompile(group.Message + "(\\(.*\\))?!?:.")
			matches := re.FindStringSubmatch(title)

			if len(matches) > 0 {
				if group.Skip {
					break
				}

				var scope string
				if len(matches) > 1 && matches[1] != "" {
					// Remove the parentheses from the captured scope
					rawScope := strings.TrimSuffix(strings.TrimPrefix(matches[1], "("), ")")
					scope = fmt.Sprintf("(**%s**)", strings.ToLower(rawScope))
				}

				// Remove prefix from the title
				cleanTitle := re.ReplaceAllString(title, "")
				words := strings.Fields(cleanTitle)
				words[0] = cases.Title(language.Und, cases.NoLower).String(words[0])
				commitMsg := strings.TrimSpace(strings.Join(append([]string{scope}, words...), " "))
				if isBreaking {
					breakingChanges = append(breakingChanges, commitMsg)
				} else {
					groupedCommits[group.Group] = append(groupedCommits[group.Group], commitMsg)
				}
				break
			}
		}
	}

	if len(breakingChanges) > 0 {
		entry += "\n### \U0001F4A5 Breaking Changes\n\n"
		for _, commit := range breakingChanges {
			entry += fmt.Sprintln("- " + commit)
		}
	}

	for _, groupName := range commitGroups {
		commits := groupedCommits[groupName.Group]
		if len(commits) > 0 {
			entry += fmt.Sprintf("\n### %s\n\n", groupName.Group)
			for _, commit := range commits {
				entry += fmt.Sprintln("- " + commit)
			}
		}
	}
	return entry
}

func getTagCommit(repo *git.Repository, tag *plumbing.Reference) *object.Commit {
	var commit *object.Commit
	// Step 1: Resolve the Tag to a Commit
	// Dereference the tag to get the commit it is pointing to
	obj, err := repo.TagObject(tag.Hash())
	if err != nil {
		// The tag might be a lightweight tag,
		// not an annotated tag. In this case,
		// it directly points to a commit.
		commit, err = repo.CommitObject(tag.Hash())
		if err != nil {
			log.Fatalln("Cannot retrieve commit from tag:", err)
		}
	} else {
		// The tag is an annotated tag, so we need to
		// further resolve the object it is pointing to.
		commit, err = obj.Commit()
		if err != nil {
			log.Fatalln("Cannot retrieve commit from tag object:", err)
		}
	}

	return commit
}

// isAncestorCommit checks if ancestor is an ancestor of descendant
func isAncestorCommit(_ *git.Repository, ancestor, descendant *object.Commit) (bool, error) {
	// If they're the same commit, ancestor is technically an ancestor
	if ancestor.Hash == descendant.Hash {
		return true, nil
	}
	
	// Walk back from descendant to see if we can reach ancestor
	found := false
	iter := object.NewCommitIterBSF(descendant, nil, nil)
	err := iter.ForEach(func(c *object.Commit) error {
		if c.Hash == ancestor.Hash {
			found = true
			return storer.ErrStop
		}
		return nil
	})
	if err != nil && err != storer.ErrStop {
		return false, err
	}
	
	return found, nil
}
