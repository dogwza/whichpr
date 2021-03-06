package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/github/hub/github"
	api "github.com/google/go-github/github"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/skratchdot/open-golang/open"
)

const Usage = `Usage:
	whichpr show|open SHA1
	whichpr version
`

var version = "master"

type ErrorMessage struct {
	message string
}

func NewErrorMessage(message string) *ErrorMessage {
	var m string
	if message == "" {
		m = Usage
	} else {
		m = fmt.Sprintf("%s\n%s", Usage, message)
	}
	return &ErrorMessage{
		message: m,
	}
}

func (e *ErrorMessage) Error() string {
	return e.message
}

func (e *ErrorMessage) Format(s fmt.State, verb rune) {
	io.WriteString(s, e.message)
}

func main() {
	if err := Main(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}

func Main(args []string) error {
	if len(args) < 2 {
		return NewErrorMessage("")
	}
	command := args[1]
	switch command {
	case "show":
		if len(args) != 3 {
			return NewErrorMessage("")
		}
		sha1 := args[2]
		return Show(sha1)
	case "open":
		if len(args) != 3 {
			return NewErrorMessage("")
		}
		sha1 := args[2]
		return Open(sha1)
	case "version":
		return Version()
	default:
		return NewErrorMessage(fmt.Sprintf("%s is unknown command", command))
	}
}

func Version() error {
	fmt.Println(version)
	return nil
}

func Show(sha1 string) error {
	prj, err := Project()
	if err != nil {
		return err
	}

	num, err := PullRequestNumber(prj, sha1)
	if err != nil {
		return err
	}

	fmt.Println(num)

	return nil
}

func Open(sha1 string) error {
	prj, err := Project()
	if err != nil {
		return err
	}

	num, err := PullRequestNumber(prj, sha1)
	if err != nil {
		return err
	}

	url := prj.WebURL("", "", fmt.Sprintf("pull/%d", num))
	return open.Run(url)
}

func PullRequestNumber(prj *github.Project, sha1 string) (int, error) {
	if len(sha1) < 7 {
		return 0, NewErrorMessage("SHA1 must be at least seven characters")
	}
	pr, err := SquashedPullReqNum(sha1)
	if err == nil {
		return pr, nil
	}
	pr, err = MergedPullRequestNum(sha1)
	if err == nil {
		return pr, nil
	}

	repo := prj.String()

	// TODO: sort
	client, err := APIClient()
	if err != nil {
		return 0, err
	}
	res, _, err := client.Search.Issues(context.Background(), fmt.Sprintf("%s is:merged repo:%s", sha1, repo), nil)
	if err != nil {
		return 0, err
	}
	if len(res.Issues) == 0 {
		return 0, errors.New("Pull Request is not found")
	}

	return *res.Issues[0].Number, nil
}

func SquashedPullReqNum(sha1 string) (int, error) {
	out, err := exec.Command("git", "log", "--pretty=format:%s", "-n", "1", sha1).Output()
	if err != nil {
		return 0, err
	}
	msg := strings.Split(string(out), "\n")[0]
	re := regexp.MustCompile(`\(\#(\d+)\)$`)
	match := re.FindStringSubmatch(msg)
	if len(match) == 0 {
		return 0, errors.New("Does not match")
	}
	return strconv.Atoi(match[1])
}

func MergedPullRequestNum(sha1 string) (int, error) {
	out, err := exec.Command("git", "log", "--merges", "--pretty=format:%P %s", "--reverse", "--ancestry-path", sha1+"..@").Output()
	if err != nil {
		return 0, err
	}
	msgs := strings.Split(string(out), "\n")
	re := regexp.MustCompile(`^(?:[a-f0-9]+ ){2}Merge pull request \#(\d+) from `)
	mergeCommit, err := findRegexp(msgs, re)
	if err != nil {
		return 0, err
	}

	pr := re.FindStringSubmatch(mergeCommit)[1]

	if isParent(sha1, regexp.MustCompile(`\w+ (\w+) `).FindStringSubmatch(mergeCommit)[1]) {
		return strconv.Atoi(pr)
	} else {
		return 0, errors.Errorf("%s is not parent", sha1)
	}
}

// RepoName returns owner/repo.
func Project() (*github.Project, error) {
	repo, err := github.LocalRepo()
	if err != nil {
		return nil, err
	}
	prj, err := repo.MainProject()
	if err != nil {
		return nil, err
	}

	return prj, nil
}

func APIClient() (*api.Client, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return nil, err
	}

	confPath := filepath.Join(homeDir, ".config", "whichpr")
	err = os.Setenv("HUB_CONFIG", confPath)
	if err != nil {
		return nil, err
	}

	c := github.CurrentConfig()
	host, err := c.DefaultHost()
	if err != nil {
		return nil, err
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: host.AccessToken},
	)
	tc := oauth2.NewClient(context.Background(), ts)
	return api.NewClient(tc), nil
}

func findRegexp(arr []string, re *regexp.Regexp) (string, error) {
	for _, str := range arr {
		if re.MatchString(str) {
			return str, nil
		}
	}
	return "", fmt.Errorf("Missing")
}

func isParent(parent, child string) bool {
	if strings.HasPrefix(child, parent) {
		return true
	}
	b, err := exec.Command("git", "log", "--ancestry-path", parent+".."+child).Output()
	if err != nil {
		return false
	}

	return len(b) != 0
}
