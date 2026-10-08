package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"strings"
	"sync"
)

func mirrorSource(t *testing.T, directory string) *git.Repository {
	t.Helper()
	repo, err := git.PlainInit(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatal(err)
	}
	return repo
}
func mirrorCommit(t *testing.T, repo *git.Repository, value string) plumbing.Hash {
	t.Helper()
	tree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree.Filesystem.Root(), "file.txt"), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Add("file.txt"); err != nil {
		t.Fatal(err)
	}
	hash, err := tree.Commit(value, &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
func mirrorTestTarget(t *testing.T) (*Target, *git.Repository, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	source := filepath.Join(t.TempDir(), "source")
	repo := mirrorSource(t, source)
	return &Target{url: "file://" + filepath.Join(t.TempDir(), "primary.git"), branch: "main", fallbackURLs: []string{"file://" + source}}, repo, source
}

func TestMirrorCloneFetchAndPrimaryRecovery(t *testing.T) {
	target, source, sourcePath := mirrorTestTarget(t)
	initial := mirrorCommit(t, source, "initial")
	if err := getRepo(target); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(getDirectory(target))
	if err != nil {
		t.Fatal(err)
	}
	head, _ := repo.Head()
	if head.Hash() != initial {
		t.Fatal("did not clone mirror")
	}
	remote, _ := repo.Remote("origin")
	if remote.Config().URLs[0] != target.url {
		t.Fatal("cache identity switched to mirror")
	}
	next := mirrorCommit(t, source, "next")
	got, err := getLatest(target)
	if err != nil || got != next {
		t.Fatalf("mirror fetch: %s %v", got, err)
	}
	// A recovered primary wins, even when it deliberately resets to an older commit.
	primaryPath := strings.TrimPrefix(target.url, "file://")
	primary, err := git.PlainClone(primaryPath, true, &git.CloneOptions{URL: sourcePath})
	if err != nil {
		t.Fatal(err)
	}
	if err := primary.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), initial)); err != nil {
		t.Fatal(err)
	}
	got, err = getLatest(target)
	if err != nil || got != initial {
		t.Fatalf("primary did not regain priority: %s %v", got, err)
	}
}

func TestMirrorRejectsStaleAndDivergentHistory(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	initial := mirrorCommit(t, source, "initial")
	if err := getClone(target); err != nil {
		t.Fatal(err)
	}
	latest := mirrorCommit(t, source, "latest")
	if _, err := getLatest(target); err != nil {
		t.Fatal(err)
	}
	ref := plumbing.NewBranchReferenceName("main")
	if err := source.Storer.SetReference(plumbing.NewHashReference(ref, initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := getLatest(target); err == nil {
		t.Fatal("stale mirror accepted")
	}
	local, _ := git.PlainOpen(getDirectory(target))
	head, _ := local.Head()
	if head.Hash() != latest {
		t.Fatal("stale mirror changed checkout")
	}
	tree, _ := source.Worktree()
	if err := tree.Checkout(&git.CheckoutOptions{Branch: ref, Force: true}); err != nil {
		t.Fatal(err)
	}
	mirrorCommit(t, source, "divergent")
	if _, err := getLatest(target); err == nil {
		t.Fatal("divergent mirror accepted")
	}
	head, _ = local.Head()
	if head.Hash() != latest {
		t.Fatal("divergent mirror changed checkout")
	}
}

func TestMirrorOrderedRetryAndCloneFailureCleanup(t *testing.T) {
	target, source, sourcePath := mirrorTestTarget(t)
	want := mirrorCommit(t, source, "initial")
	target.fallbackURLs = []string{target.url, "", "file://" + filepath.Join(t.TempDir(), "missing"), "file://" + sourcePath, "file://" + sourcePath}
	if len(repositoryURLs(target)) != 3 {
		t.Fatal("empty or duplicate sources retained")
	}
	if err := getClone(target); err != nil {
		t.Fatal(err)
	}
	got, err := getLatest(target)
	if err != nil || got != want {
		t.Fatalf("ordered fallback failed: %v", err)
	}
	if err := os.RemoveAll(getDirectory(target)); err != nil {
		t.Fatal(err)
	}
	target.fallbackURLs = []string{"file://" + filepath.Join(t.TempDir(), "missing")}
	if err := getRepo(target); err == nil {
		t.Fatal("all-source clone failure ignored")
	}
	if _, err := os.Stat(getDirectory(target)); !os.IsNotExist(err) {
		t.Fatal("partial destination left behind")
	}
	stages, _ := filepath.Glob(".fetchit-clone-*")
	if len(stages) != 0 {
		t.Fatal("failed clone stages leaked")
	}
	target.fallbackURLs = []string{"file://" + sourcePath}
	if err := getClone(target); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
}

func TestMirrorConfigAndIdentity(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("targetConfigs:\n- url: https://primary.example/repo.git\n  fallbackURLs: [https://mirror.example/repo.git]\n  raw:\n  - name: example\n")); err != nil {
		t.Fatal(err)
	}
	var config FetchitConfig
	if err := v.Unmarshal(&config); err != nil {
		t.Fatal(err)
	}
	engine := getMethodTargetScheds(config.TargetConfigs, newFetchit())
	for method := range engine.methodTargetScheds {
		target := method.GetTarget()
		if len(target.fallbackURLs) != 1 || target.url != config.TargetConfigs[0].Url {
			t.Fatal("source configuration lost")
		}
		before := getDirectory(target)
		target.fallbackURLs = []string{"file:///another/repo.git"}
		if getDirectory(target) != before {
			t.Fatal("mirror changed cache identity")
		}
	}
}

func TestMirrorSignatureAndMissingBranchFailures(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	mirrorCommit(t, source, "unsigned")
	target.gitsignVerify = true
	if err := getClone(target); err == nil {
		t.Fatal("unsigned mirrored clone accepted")
	}
	if _, err := os.Stat(getDirectory(target)); !os.IsNotExist(err) {
		t.Fatal("rejected signed clone left a checkout")
	}
	target.gitsignVerify = false
	target.branch = "missing"
	if err := getClone(target); err == nil {
		t.Fatal("missing branch clone accepted")
	}
	target.branch = "main"
	if err := getClone(target); err != nil {
		t.Fatal(err)
	}
	// Invalid primary URL and an unsigned mirror must not move the local branch.
	target.gitsignVerify = true
	mirrorCommit(t, source, "another unsigned commit")
	if _, err := getLatest(target); err == nil {
		t.Fatal("unsigned mirror update accepted")
	}
	repo, _ := git.PlainOpen(getDirectory(target))
	head, _ := repo.Head()
	commit, _ := repo.CommitObject(head.Hash())
	if commit.Message != "unsigned" {
		t.Fatal("signature failure advanced branch")
	}
	target.fallbackURLs = nil
	if len(repositoryURLs(target)) != 1 {
		t.Fatal("single-source configuration changed")
	}
}

func TestMirrorPreferenceWithMultipleUsableSources(t *testing.T) {
	target, first, firstPath := mirrorTestTarget(t)
	initial := mirrorCommit(t, first, "initial")
	secondPath := filepath.Join(t.TempDir(), "second")
	second, err := git.PlainClone(secondPath, false, &git.CloneOptions{URL: firstPath})
	if err != nil {
		t.Fatal(err)
	}
	target.fallbackURLs = []string{"file://" + firstPath, "file://" + secondPath}
	if err := getClone(target); err != nil {
		t.Fatal(err)
	}
	repo, _ := git.PlainOpen(getDirectory(target))
	head, _ := repo.Head()
	if head.Hash() != initial {
		t.Fatal("initial clone preference changed")
	}
	preferred := mirrorCommit(t, first, "preferred")
	mirrorCommit(t, second, "second equally usable source")
	got, err := getLatest(target)
	if err != nil || got != preferred {
		t.Fatalf("source order not respected: %s %v", got, err)
	}
}

func TestMirrorURLPolicyAndNormalizedSelection(t *testing.T) {
	for _, tc := range []struct {
		url        string
		credential bool
		valid      bool
	}{
		{"https://mirror.example/repo.git", true, true},
		{"http://mirror.example/repo.git", false, true},
		{"http://mirror.example/repo.git", true, false},
		{"https://user:secret@mirror.example/repo.git", false, false},
		{"ssh://git@mirror.example/repo.git", true, true},
		{"git@mirror.example:repo.git", true, true},
		{"file:///opt/mirrors/repo.git", false, true},
		{"file://untrusted-host/opt/repo.git", false, false},
		{"git://mirror.example/repo.git", false, false},
		{"", false, false},
		{" /opt/repo.git ", false, false},
	} {
		target := &Target{url: tc.url}
		if tc.credential {
			target.pat = "temporary-secret"
		}
		err := validateRepositorySources(target)
		if (err == nil) != tc.valid {
			t.Fatalf("URL policy %q: %v", tc.url, err)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatal("credential leaked in validation error")
		}
	}
	target := &Target{url: "https://primary.example/repo.git", fallbackURLs: []string{"", "https://primary.example/repo.git"}}
	if hasRepositoryMirrors(target) {
		t.Fatal("duplicates enabled shortened mirror timeout")
	}
	cause := context.DeadlineExceeded
	err := &mirrorSourceError{Source: 2, Operation: "fetch", Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("source error loses timeout classification")
	}
}

func TestMirrorConcurrentFetchesShareCacheSafely(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	mirrorCommit(t, source, "initial")
	if err := getClone(target); err != nil {
		t.Fatal(err)
	}
	want := mirrorCommit(t, source, "updated")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			got, err := getLatest(target)
			if err != nil || got != want {
				t.Errorf("concurrent fetch: %s %v", got, err)
			}
		})
	}
	wg.Wait()
	repo, _ := git.PlainOpen(getDirectory(target))
	refs, err := repo.References()
	if err != nil {
		t.Fatal(err)
	}
	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		if strings.HasPrefix(ref.Name().String(), "refs/fetchit/incoming/") {
			t.Error("temporary ref leaked")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
