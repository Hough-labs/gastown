package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLookupPullRequestRecordedURLSurvivesDeletedHead(t *testing.T) {
	installFakeGH(t, `#!/bin/sh
case "$*" in
  *baseRepository*) printf 'unsupported field requested: %s\n' "$*" >&2; exit 2 ;;
esac
if [ "$1" = "pr" ] && [ "$2" = "view" ] && [ "$3" = "https://github.com/upstream/repo/pull/42" ]; then
  printf '%s\n' '{"number":42,"url":"https://github.com/upstream/repo/pull/42","state":"MERGED","mergedAt":"2026-07-13T12:00:00Z","headRefName":"fix/deleted-head","headRefOid":"abc123","headRepository":null,"headRepositoryOwner":{"login":"fork-owner"},"baseRepository":{"nameWithOwner":"upstream/repo"}}'
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	pr, err := g.LookupPullRequest(PullRequestRef{URL: "https://github.com/upstream/repo/pull/42", Branch: "fix/deleted-head"})
	if err != nil {
		t.Fatalf("LookupPullRequest: %v", err)
	}
	if !pr.Merged() || pr.Number != 42 || pr.HeadOwner != "fork-owner" {
		t.Fatalf("unexpected PR: %+v", pr)
	}
	if pr.LookupSource != "recorded-url" {
		t.Fatalf("LookupSource = %q, want recorded-url", pr.LookupSource)
	}
}

func TestLookupPullRequestQualifiedForkHead(t *testing.T) {
	installFakeGH(t, `#!/bin/sh
if [ "$1" = "api" ] && [ "$2" = "-X" ] && [ "$3" = "GET" ] && [ "$4" = "repos/upstream/repo/pulls" ]; then
  printf '%s\n' '[{"number":4474,"html_url":"https://github.com/upstream/repo/pull/4474","state":"open","merged_at":null,"head":{"ref":"fix/fork-head","sha":"sha4474","repo":{"full_name":"blairsilverberg/repo","owner":{"login":"blairsilverberg"}},"user":{"login":"blairsilverberg"}},"base":{"repo":{"full_name":"upstream/repo"}}}]'
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	pr, err := g.LookupPullRequest(PullRequestRef{Branch: "fix/fork-head", HeadOwner: "blairsilverberg"})
	if err != nil {
		t.Fatalf("LookupPullRequest: %v", err)
	}
	if !pr.Open() || pr.Number != 4474 || pr.HeadOwner != "blairsilverberg" {
		t.Fatalf("unexpected PR: %+v", pr)
	}
	if pr.LookupSource != "qualified-head" {
		t.Fatalf("LookupSource = %q, want qualified-head", pr.LookupSource)
	}
}

func TestLookupPullRequestBranchAmbiguityFailsClosed(t *testing.T) {
	installFakeGH(t, `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  printf '%s\n' '[{"number":1,"url":"https://github.com/upstream/repo/pull/1","state":"OPEN","mergedAt":"","headRefName":"shared","headRefOid":"a1","headRepository":{"nameWithOwner":"one/repo"},"headRepositoryOwner":{"login":"one"},"baseRepository":{"nameWithOwner":"upstream/repo"}},{"number":2,"url":"https://github.com/upstream/repo/pull/2","state":"OPEN","mergedAt":"","headRefName":"shared","headRefOid":"b2","headRepository":{"nameWithOwner":"two/repo"},"headRepositoryOwner":{"login":"two"},"baseRepository":{"nameWithOwner":"upstream/repo"}}]'
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	_, err := g.LookupPullRequest(PullRequestRef{Branch: "shared"})
	if !errors.Is(err, ErrPullRequestAmbiguous) {
		t.Fatalf("LookupPullRequest err = %v, want ErrPullRequestAmbiguous", err)
	}
	if !g.HasOpenPR("shared") {
		t.Fatal("HasOpenPR should protect branch deletion on ambiguous lookup")
	}
}

func TestLookupPullRequestBranchHeadSHADisambiguates(t *testing.T) {
	installFakeGH(t, `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  printf '%s\n' '[{"number":1,"url":"https://github.com/upstream/repo/pull/1","state":"CLOSED","mergedAt":"","headRefName":"shared","headRefOid":"old","headRepository":{"nameWithOwner":"one/repo"},"headRepositoryOwner":{"login":"one"},"baseRepository":{"nameWithOwner":"upstream/repo"}},{"number":2,"url":"https://github.com/upstream/repo/pull/2","state":"CLOSED","mergedAt":"2026-07-13T12:00:00Z","headRefName":"shared","headRefOid":"wanted","headRepository":{"nameWithOwner":"two/repo"},"headRepositoryOwner":{"login":"two"},"baseRepository":{"nameWithOwner":"upstream/repo"}}]'
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	pr, err := g.LookupPullRequest(PullRequestRef{Branch: "shared", HeadSHA: "wanted"})
	if err != nil {
		t.Fatalf("LookupPullRequest: %v", err)
	}
	if pr.Number != 2 || !pr.Merged() {
		t.Fatalf("unexpected PR: %+v", pr)
	}
}

func TestFindPRNumberRequiresOpenPR(t *testing.T) {
	installFakeGH(t, `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "view" ] && [ "$3" = "99" ]; then
  printf '%s\n' '{"number":99,"url":"https://github.com/upstream/repo/pull/99","state":"CLOSED","mergedAt":"","headRefName":"closed","headRefOid":"abc","headRepository":{"nameWithOwner":"fork/repo"},"headRepositoryOwner":{"login":"fork"},"baseRepository":{"nameWithOwner":"upstream/repo"}}'
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	number, err := g.FindPRNumberForRef(PullRequestRef{Number: 99})
	if err != nil {
		t.Fatalf("FindPRNumberForRef: %v", err)
	}
	if number != 0 {
		t.Fatalf("FindPRNumberForRef = %d, want 0 for closed-unmerged PR", number)
	}
}

func TestPullRequestApprovalAndMergeUseResolvedURLAndRepo(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "gh.log")
	t.Setenv("GH_LOG", logPath)
	installFakeGH(t, `#!/bin/sh
printf '%s\n' "$*" >> "$GH_LOG"
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  printf '%s\n' '{"reviews":[{"author":{"login":"reviewer1"},"state":"APPROVED"}]}'
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "merge" ]; then
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	dir := initTestRepo(t)
	g := NewGit(dir)
	pr := &PullRequestInfo{Number: 42, URL: "https://github.com/upstream/repo/pull/42", BaseRepo: "upstream/repo"}

	approved, err := g.IsPullRequestApproved(pr)
	if err != nil {
		t.Fatalf("IsPullRequestApproved: %v", err)
	}
	if !approved {
		t.Fatal("IsPullRequestApproved = false, want true")
	}
	if _, err := g.GhPrMergePullRequest(pr, "squash"); err != nil {
		t.Fatalf("GhPrMergePullRequest: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"pr view https://github.com/upstream/repo/pull/42 --json reviews --repo upstream/repo",
		"pr merge https://github.com/upstream/repo/pull/42 --squash --delete-branch --repo upstream/repo",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("gh log missing %q\nlog:\n%s", want, log)
		}
	}
}

func TestHasReviewDecisionSince(t *testing.T) {
	// gh returns one CHANGES_REQUESTED at 22:00 and one APPROVED at 22:50.
	installFakeGH(t, `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  printf '%s\n' '{"reviews":[{"state":"COMMENTED","submittedAt":"2026-07-19T21:00:00Z"},{"state":"CHANGES_REQUESTED","submittedAt":"2026-07-19T22:00:00Z"},{"state":"APPROVED","submittedAt":"2026-07-19T22:50:38Z"}]}'
  exit 0
fi
printf 'unexpected gh args: %s\n' "$*" >&2
exit 1
`)
	g := NewGit(initTestRepo(t))
	pr := &PullRequestInfo{Number: 243, URL: "https://github.com/upstream/repo/pull/243"}

	mustParse := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return ts
	}

	cases := []struct {
		name  string
		since string
		want  bool
	}{
		// Assigned before both decisions — the review happened after assignment.
		{"decision after assignment", "2026-07-19T20:00:00Z", true},
		// The PR advanced and was re-reviewed; assigned just before the approval.
		{"approval after advance", "2026-07-19T22:30:00Z", true},
		// Assigned after the last decision — no fresh review; must not satisfy.
		{"assigned after last decision", "2026-07-19T23:00:00Z", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.HasReviewDecisionSince(pr, mustParse(tc.since))
			if err != nil {
				t.Fatalf("HasReviewDecisionSince: %v", err)
			}
			if got != tc.want {
				t.Fatalf("HasReviewDecisionSince(since=%s) = %v, want %v", tc.since, got, tc.want)
			}
		})
	}
}

func TestHasReviewDecisionSinceIgnoresNonDecisions(t *testing.T) {
	// Only COMMENTED/DISMISSED reviews exist — none is a decision, so no evidence.
	installFakeGH(t, `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  printf '%s\n' '{"reviews":[{"state":"COMMENTED","submittedAt":"2026-07-19T22:00:00Z"},{"state":"DISMISSED","submittedAt":"2026-07-19T22:30:00Z"}]}'
  exit 0
fi
exit 1
`)
	g := NewGit(initTestRepo(t))
	since, _ := time.Parse(time.RFC3339, "2026-07-19T20:00:00Z")
	got, err := g.HasReviewDecisionSince(&PullRequestInfo{URL: "https://github.com/upstream/repo/pull/243"}, since)
	if err != nil {
		t.Fatalf("HasReviewDecisionSince: %v", err)
	}
	if got {
		t.Fatal("HasReviewDecisionSince = true for comment-only reviews, want false")
	}
}

func installFakeGH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func addGitHubRemotes(t *testing.T, g *Git) {
	t.Helper()
	if _, err := g.AddRemote("origin", "https://github.com/fork/repo.git"); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("AddRemote origin: %v", err)
	}
	if err := g.AddUpstreamRemote("https://github.com/upstream/repo.git"); err != nil {
		t.Fatalf("AddUpstreamRemote: %v", err)
	}
}
