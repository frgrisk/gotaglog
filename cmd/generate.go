package cmd

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/glamour"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

	// re is always non-nil: matchGroup and skipGroup are the only ways to build a
	// CommitGroup, and a zero-value one would panic when categorizing a commit.
	re *regexp.Regexp
}

// matchGroup builds a group whose commits are listed under name.
func matchGroup(message, name string) CommitGroup {
	return CommitGroup{Message: message, Group: name, re: mustPrefixRE(message)}
}

// skipGroup builds a group whose commits are left out of the changelog.
func skipGroup(message string) CommitGroup {
	return CommitGroup{Message: message, Skip: true, re: mustPrefixRE(message)}
}

// mustPrefixRE compiles message into a conventional-commit prefix matcher:
// an optional (scope), an optional breaking "!", the colon, and any spacing
// before the description. The spacing must stay \s* rather than a wildcard, or
// "fix:no space" loses its first character to the match.
func mustPrefixRE(message string) *regexp.Regexp {
	return regexp.MustCompile(message + `(\(.*\))?!?:\s*`)
}

var commitGroups = []CommitGroup{
	matchGroup("^feat", "✨ Features"),
	matchGroup("^fix", "🐛 Fixes"),
	matchGroup("^docs", "📖 Documentation"),
	matchGroup("^perf", "⚡️Performance"),
	matchGroup("^refactor", "✏️ Refactor"),
	matchGroup("^revert", "↩️ Revert"),
	matchGroup("^style", "Styling"),
	matchGroup("^test", "🧪 Testing"),
	matchGroup("^build\\(deps\\)", "⚙️ Dependencies"),
	matchGroup("^build\\(deps-dev\\)", "⚙️ Dev Dependencies"),
	matchGroup("^build", "🛠️ Build System"),
	matchGroup("^ci", "🔄 Continuous Integration"),
	skipGroup("^chore\\(release\\)"),
	skipGroup("^chore\\(ignore\\)"),
	matchGroup("^chore", "Miscellaneous Tasks"),
}

// titleCaser upper-cases the first word of a commit description. Building one
// per commit costs an allocation for no benefit; it is stateless and reusable.
var titleCaser = cases.Title(language.Und, cases.NoLower)

// unreleasedHeader renders the changelog heading for not-yet-released commits.
// base is the newest released version, which the --inc-* flags increment from
// and which a caller-supplied --tag is sanity-checked against.
func unreleasedHeader(base *semver.Version) string {
	today := time.Now().Format("2006-01-02")
	tag := viper.GetString("tag")

	switch {
	case viper.GetBool("inc-major"):
		return fmt.Sprintf("## [%s] - %s", base.IncMajor(), today)
	case viper.GetBool("inc-minor"):
		return fmt.Sprintf("## [%s] - %s", base.IncMinor(), today)
	case viper.GetBool("inc-patch"):
		return fmt.Sprintf("## [%s] - %s", base.IncPatch(), today)
	case tag != defaultUnreleasedTag:
		ver, err := semver.NewVersion(tag)
		if err != nil {
			log.WithField("tag", tag).Fatal(err)
		}
		if ver.LessThan(base) {
			log.Warnf("Unreleased tag %q is lower than existing tag %q in the repository.", ver, base)
		}
		if ver.Equal(base) {
			log.Warnf("Unreleased tag %q already exists in the repository.", ver)
		}
		return fmt.Sprintf("## [%s] - %s", ver, today)
	default:
		return fmt.Sprintf("## [%s]", tag)
	}
}

func getChangeLog() {
	repoPath := viper.GetString("repo")
	if repoPath == "" {
		log.Fatalln("Repository path is empty")
		return
	}
	repoPath = filepath.Clean(repoPath)
	log.Debugf("Repository path is set to %q", repoPath)
	repo, closeRepo, err := openRepository(repoPath)
	if err != nil {
		log.Fatalln("Cannot open repository:", err)
		return
	}
	defer closeRepo()

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

	slices.SortFunc(semverTags, (*semver.Version).Compare)

	var prevTag *plumbing.Reference

	var changelog []string

	// Find the most recent tag that is an ancestor of HEAD
	head, err := repo.Head()
	if err != nil {
		log.Fatalln("Cannot resolve HEAD:", err)
	}

	// Filter tags to only include those that are ancestors of HEAD
	// This ensures we don't include tags from other branches when generating
	// a changelog from a specific branch (e.g., v0.10 branch shouldn't include
	// tags from v25.x branch)
	var ancestorTags semver.Collection
	ancestorTagMap := make(map[string]*plumbing.Reference)

	graph := newCommitGraph(repo)

	// Walk back from HEAD once. Testing each tag separately would re-walk history
	// from HEAD per tag, which is quadratic in repositories with many tags.
	headReachable, err := graph.ancestors(head.Hash())
	if err != nil {
		log.Fatalln("Cannot walk history from HEAD:", err)
	}

	for _, ver := range semverTags {
		tag := tagMap[ver.String()]
		if headReachable[graph.tagCommit(tag).Hash] {
			ancestorTags = append(ancestorTags, ver)
			ancestorTagMap[ver.String()] = tag
		}
	}

	// Re-sort the filtered tags
	slices.SortFunc(ancestorTags, (*semver.Version).Compare)

	var lastAncestorTag *plumbing.Reference
	var lastAncestorVer *semver.Version
	if len(ancestorTags) > 0 {
		lastAncestorVer = ancestorTags[len(ancestorTags)-1]
		lastAncestorTag = ancestorTagMap[lastAncestorVer.String()]
	}

	// If --unreleased flag is set, only generate unreleased changes
	if viper.GetBool("unreleased") && lastAncestorTag != nil {
		entry := getTagEntryDetails(graph, lastAncestorTag, nil)
		if entry != "" {
			changelog = []string{"# Changelog\n", unreleasedHeader(lastAncestorVer), entry}
		} else {
			changelog = []string{"# Changelog\n"}
		}
	} else {
		// Regular changelog generation
		for _, ver := range ancestorTags {
			tag := ancestorTagMap[ver.String()]
			entry := fmt.Sprintf("## [%s] - %s\n", ver.String(), graph.tagCommit(tag).Author.When.Format("2006-01-02"))
			entry += getTagEntryDetails(graph, prevTag, tag)
			changelog = append([]string{entry}, changelog...)
			prevTag = tag
			if lastAncestorTag != nil && ver == lastAncestorVer {
				entry = getTagEntryDetails(graph, tag, nil)
				if entry != "" {
					changelog = append([]string{unreleasedHeader(ver), entry}, changelog...)
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
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			width = min(uint(w), 120)
		}
	}
	width = cmp.Or(width, 80)

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
func getCommitsInRange(g *commitGraph, olderTag, newerTag *plumbing.Reference) ([]*object.Commit, error) {
	var until plumbing.Hash

	if newerTag != nil {
		until = g.tagCommit(newerTag).Hash
	} else {
		head, err := g.repo.Head()
		if err != nil {
			return nil, err
		}
		until = head.Hash()
	}

	// Get all commits reachable from olderTag (if any)
	var olderCommits map[plumbing.Hash]bool
	if olderTag != nil {
		var err error
		if olderCommits, err = g.ancestors(g.tagCommit(olderTag).Hash); err != nil {
			return nil, err
		}
	}

	// Get commits reachable from until that are not in olderCommits. Passing
	// olderCommits as seen prunes the walk at the tag boundary instead of
	// traversing to the root and filtering; olderCommits is closed under
	// ancestry, so nothing reachable only through it is lost.
	var commits []*object.Commit
	_, err := g.walk(until, olderCommits, 0, func(h plumbing.Hash) error {
		c, err := g.repo.CommitObject(h)
		if err != nil {
			return err
		}
		commits = append(commits, c)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return commits, nil
}

func getTagEntryDetails(g *commitGraph, olderTag, newerTag *plumbing.Reference) string {
	// Get commits that are in this specific tag range
	commits, err := getCommitsInRange(g, olderTag, newerTag)
	if err != nil {
		log.Fatalln("Cannot get commits in range:", err)
	}

	var entry strings.Builder

	groupedCommits := make(map[string][]string)
	var breakingChanges []string

	for _, c := range commits {
		// Only print the first line of the commit message (the title)
		title, _, _ := strings.Cut(c.Message, "\n")
		isBreaking := strings.Contains(title, "!:") ||
			strings.Contains(strings.ToLower(c.Message), "breaking change:") ||
			strings.Contains(strings.ToLower(c.Message), "breaking-change:")

		for _, group := range commitGroups {
			matches := group.re.FindStringSubmatch(title)

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
				cleanTitle := group.re.ReplaceAllString(title, "")
				words := strings.Fields(cleanTitle)
				// The prefix match consumes one character past the colon, so a
				// title that is only a prefix ("fix: ") leaves no words behind.
				if len(words) == 0 {
					log.Debugf("Skipping commit %s: no description after %q prefix", c.Hash.String()[:7], group.Message)
					break
				}
				words[0] = titleCaser.String(words[0])
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
		entry.WriteString("\n### \U0001F4A5 Breaking Changes\n\n")
		for _, commit := range breakingChanges {
			fmt.Fprintln(&entry, "- "+commit)
		}
	}

	for _, groupName := range commitGroups {
		commits := groupedCommits[groupName.Group]
		if len(commits) > 0 {
			fmt.Fprintf(&entry, "\n### %s\n\n", groupName.Group)
			for _, commit := range commits {
				fmt.Fprintln(&entry, "- "+commit)
			}
		}
	}
	return entry.String()
}
