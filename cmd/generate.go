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

	var changelog []string

	for _, ver := range semverTags {
		tag := tagMap[ver.String()]
		entry := fmt.Sprintf("## [%s] - %s\n", ver.String(), getTagCommit(repo, tag).Author.When.Format("2006-01-02"))
		entry += getTagEntryDetails(repo, prevTag, tag)
		changelog = append([]string{entry}, changelog...)
		prevTag = tag
		if ver == semverTags[len(semverTags)-1] {
			entry = getTagEntryDetails(repo, tag, nil)
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
				if viper.GetBool("unreleased") {
					changelog = unreleasedEntry
				} else {
					changelog = append(unreleasedEntry, changelog...)
				}
			}
		}
	}
	changelog = append([]string{"# Changelog\n"}, changelog...)
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

func getTagEntryDetails(repo *git.Repository, olderTag, newerTag *plumbing.Reference) string {
	var from, until *object.Commit
	options := &git.LogOptions{}

	if olderTag != nil {
		from = getTagCommit(repo, olderTag)
	}

	if newerTag != nil {
		until = getTagCommit(repo, newerTag)
		options = &git.LogOptions{From: until.Hash}
	}

	var entry string

	commitIter, err := repo.Log(options)
	if err != nil {
		log.Fatalln("Cannot fetch commits:", err)
	}

	groupedCommits := make(map[string][]string)

	_ = commitIter.ForEach(func(c *object.Commit) error {
		ancestor := false
		if olderTag != nil {
			ancestor, err = c.IsAncestor(getTagCommit(repo, olderTag))
			if err != nil {
				log.Fatalf("Cannot check ancestor of commit %s: %v", getTagCommit(repo, olderTag).Hash, err)
			}
		}
		if (from != nil && c.Hash == from.Hash) || ancestor {
			return storer.ErrStop
		}

		// Only print the first line of the commit message (the title)
		title := strings.Split(c.Message, "\n")[0]

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
				groupedCommits[group.Group] = append(groupedCommits[group.Group], strings.TrimSpace(strings.Join(append([]string{scope}, words...), " ")))
				break
			}
		}

		return nil
	})

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
