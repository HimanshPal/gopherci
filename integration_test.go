//+build integration

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-github/github"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

// IntegrationTest helps run a single integration test. Focusing on interaction
// between GopherCI and GitHub, IntegrationTest helps write tests that ensures
// GopherCI receives hooks, detects issues and posts comments.
type IntegrationTest struct {
	t             *testing.T
	workdir       string
	tmpdir        string
	owner         string
	repo          string
	github        *github.Client
	gciCancelFunc context.CancelFunc
	env           []string
}

// NewIntegrationTest creates an environment for running integration tests by
// creating file system temporary directories, starting gopherci and setting
// up a github repository. Must be closed when finished.
func NewIntegrationTest(t *testing.T) *IntegrationTest {
	// Load environment from .env, ignore errors as it's optional and dev only
	_ = godotenv.Load()

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not get working dir: %s", err)
	}

	// Make a temp dir which will be our github repository.
	tmpdir, err := ioutil.TempDir("", "gopherci-integration")
	if err != nil {
		t.Fatalf("could not create temporary directory: %v", err)
	}

	it := &IntegrationTest{t: t, workdir: workdir, tmpdir: tmpdir, owner: "bf-test", repo: "gopherci-itest"} // TODO config
	it.t.Logf("GitHub owner %q repo %q tmpdir %q workdir %q", it.owner, it.repo, it.tmpdir, it.workdir)

	// Force git to use our SSH key, requires Git 2.3+.
	if os.Getenv("INTEGRATION_GITHUB_KEY_FILE") != "" {
		it.env = append(it.env, fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %v", os.Getenv("INTEGRATION_GITHUB_KEY_FILE")))
	}

	// Look for binaries (just git currently) in an alternative path.
	if os.Getenv("INTEGRATION_PATH") != "" {
		it.env = append(it.env, "PATH="+os.Getenv("INTEGRATION_PATH"))
	}
	it.t.Logf("Additional environment for os/exec.Command (maybe empty): %v", it.env)

	// Setup GitHub Client.
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("INTEGRATION_GITHUB_PAT")},
	)
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	it.github = github.NewClient(tc)
	it.t.Logf("GitHub Personal Access Token len %v", len(os.Getenv("INTEGRATION_GITHUB_PAT")))

	// Obtain the clone URL and also test the personal access token.
	repo, _, err := it.github.Repositories.Get(it.owner, it.repo)
	if err != nil {
		it.t.Fatalf("could not get repository information for %v/%v using personal access token: %v", it.owner, it.repo, err)
	}

	// Initialise the repository to known good state.
	it.Exec("init.sh", *repo.SSHURL)

	// Start GopherCI.
	it.gciCancelFunc = it.startGopherCI()

	return it
}

// startGopherCI runs gopherci in the background and returns a function to be
// called when it should be terminated. Writes output to test log functions
// so they should only appear if the test fails.
func (it *IntegrationTest) startGopherCI() context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		out, err := exec.CommandContext(ctx, "gopherci").CombinedOutput()
		it.t.Logf("Gopherci output:\n%s", out)
		it.t.Logf("Gopherci error: %v", err)
	}()
	time.Sleep(10 * time.Second) // Wait for gopherci to listen on interface.
	it.t.Log("Started gopherci")
	return cancel
}

// Close stops gopherci and removes any temporary directories.
func (it *IntegrationTest) Close() {
	it.gciCancelFunc() // Kill gopherci.

	// We sleep a moment here to give the goroutine that was running gopherci
	// a chance to write its output to the tests's log function before the
	// entire test is terminated.
	time.Sleep(time.Second)

	if err := os.RemoveAll(it.tmpdir); err != nil {
		log.Printf("integration test close: could not remove %v: %v", it.tmpdir, err)
	}
}

// Exec executes a script within the testdata directory with args.
func (it *IntegrationTest) Exec(script string, args ...string) {
	cmd := exec.Command(filepath.Join(it.workdir, "testdata", script), args...)
	cmd.Env = it.env
	cmd.Dir = it.tmpdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		it.t.Fatalf("could not run %v: %v, output:\n%s", cmd.Args, err, out)
	}
	it.t.Logf("executed %v", cmd.Args)
}

// WaitForSuccess waits for the ref's Status API to be success, will only wait
// for a short timeout, unless the status is failure or error, in which case
// the test is marked as failed.
func (it *IntegrationTest) WaitForSuccess(ref string) {
	timeout := 60 * time.Second
	start := time.Now()
	for time.Now().Before(start.Add(timeout)) {
		statuses, _, err := it.github.Repositories.GetCombinedStatus(it.owner, it.repo, ref, nil)
		if err != nil {
			it.t.Fatalf("could not get combined statuses: %v", err)
		}

		for _, status := range statuses.Statuses {
			if *status.Context != "ci/gopherci/pr" {
				continue
			}
			it.t.Logf("Checking status: %v", *status.State)

			switch *status.State {
			case "success":
				return
			case "failure", "error":
				it.t.Fatalf("status %v for ref %v", ref)
			}
		}
		time.Sleep(time.Second)
	}
	it.t.Fatalf("timeout waiting for status api to be success, failure or error")
}

func TestGitHubComments(t *testing.T) {
	it := NewIntegrationTest(t)
	defer it.Close()

	// Push a branch which contains issues
	branch := "issue-comments"
	it.Exec("issue-comments.sh", branch)

	// Make PR
	pr, _, err := it.github.PullRequests.Create(it.owner, it.repo, &github.NewPullRequest{
		Title: github.String("pr title"),
		Head:  github.String(branch),
		Base:  github.String("master"),
	})
	if err != nil {
		t.Fatalf("could not create pull request: %v", err)
	}

	it.WaitForSuccess(branch)

	time.Sleep(5 * time.Second) // wait for comments to appear

	// Make sure the expected comments appear
	comments, _, err := it.github.PullRequests.ListComments(it.owner, it.repo, *pr.Number, nil)
	if err != nil {
		t.Fatalf("could not get pull request comments: %v", err)
	}

	if want := 1; len(comments) != want {
		t.Fatalf("have %v comments want %v", len(comments), want)
	}
	if want := 2; *comments[0].Position != want {
		t.Fatalf("have comments position %v want %v", *comments[0].Position, want)
	}
	if want := "golint: exported function Foo should have comment or be unexported"; *comments[0].Body != want {
		t.Fatalf("unexpected comment body:\nhave: %q\nwant: %q", *comments[0].Body, want)
	}
	if want := "foo.go"; *comments[0].Path != want {
		t.Fatalf("have comments path %q want %q", *comments[0].Path, want)
	}
}