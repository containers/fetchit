package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func repositoryURLs(target *Target) []string {
	urls := []string{target.url}
	seen := map[string]struct{}{target.url: {}}
	for _, url := range target.fallbackURLs {
		if _, exists := seen[url]; url != "" && !exists {
			urls = append(urls, url)
			seen[url] = struct{}{}
		}
	}
	return urls
}

// Clone each candidate in a separate staging directory so a failed clone never
// leaves a partial repository that prevents the next candidate or later retry.
func getCloneWithMirrors(target *Target) error {
	if err := validateRepositorySources(target); err != nil {
		return err
	}
	destination, err := filepath.Abs(getDirectory(target))
	if err != nil {
		return err
	}
	if _, err := os.Stat(destination); err == nil {
		_, err := git.PlainOpen(destination)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	if target.envSecret != "" {
		target.pat = os.Getenv(target.envSecret)
	}
	options, err := mirrorCloneOptions(target)
	if err != nil {
		return err
	}
	var failures []error
	for index, url := range repositoryURLs(target) {
		stage, err := os.MkdirTemp(filepath.Dir(destination), ".fetchit-clone-*")
		if err != nil {
			return err
		}
		options.URL = url
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		repo, cloneErr := git.PlainCloneContext(ctx, stage, false, options)
		if cloneErr == nil {
			cloneErr = verifyClone(ctx, repo, stage, target)
		}
		cancel()
		if cloneErr == nil {
			cloneErr = setPrimaryOrigin(repo, target.url)
		}
		if cloneErr == nil {
			cloneErr = os.Rename(stage, destination)
		}
		if cloneErr == nil {
			logger.Infof("Cloned repository using source %d", index+1)
			return nil
		}
		if cleanupErr := os.RemoveAll(stage); cleanupErr != nil {
			logger.Warnf("Clean up clone stage for source %d at %s: %v", index+1, stage, cleanupErr)
		}
		// Do not include URLs or transport error text: either can contain credentials.
		failures = append(failures, &mirrorSourceError{Source: index + 1, Operation: "clone", Err: cloneErr})
	}
	return errors.Join(failures...)
}

// Fetch into a temporary ref. Rejected or failed mirrors never move the tracked
// branch. Primary changes retain the existing force-fetch behavior.
func fetchWithMirrors(repo *git.Repository, target *Target, original *git.FetchOptions) error {
	if err := validateRepositorySources(target); err != nil {
		return err
	}
	branch := plumbing.NewBranchReferenceName(target.branch)
	incoming := plumbing.ReferenceName("refs/fetchit/incoming/" + rand.Text() + "/" + target.branch)
	defer func() {
		if err := repo.Storer.RemoveReference(incoming); err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
			logger.Warnf("Remove temporary fetch ref %s: %v", incoming, err)
		}
	}()
	var failures []error
	for index, url := range repositoryURLs(target) {
		expected, err := repo.Reference(branch, true)
		if err != nil {
			return err
		}
		if err := repo.Storer.RemoveReference(incoming); err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return err
		}
		options := *original
		options.RefSpecs = []config.RefSpec{config.RefSpec("+" + branch.String() + ":" + incoming.String())}
		options.RemoteName = "fetchit-source"
		// Applied-state tags are local bookkeeping; mirrors must not import tags.
		options.Tags = git.NoTags
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		candidate, err := fetchCandidate(ctx, repo, url, &options, incoming)
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			cancel()
			failures = append(failures, &mirrorSourceError{Source: index + 1, Operation: "fetch", Err: err})
			continue
		}
		if index > 0 {
			current := expected
			if current.Hash() != candidate.Hash() {
				oldCommit, err := repo.CommitObject(current.Hash())
				if err != nil {
					return err
				}
				newCommit, err := repo.CommitObject(candidate.Hash())
				if err != nil {
					return err
				}
				ahead, err := oldCommit.IsAncestor(newCommit)
				if err != nil {
					return err
				}
				if !ahead {
					failures = append(failures, fmt.Errorf("repository source %d is stale or divergent", index+1))
					continue
				}
			}
		}
		if target.gitsignVerify {
			commit, err := repo.CommitObject(candidate.Hash())
			if err != nil {
				return err
			}
			if err := VerifyGitsign(ctx, commit, candidate.Hash().String()[:hashReportLen], getDirectory(target), target.gitsignRekorURL); err != nil {
				failures = append(failures, &mirrorSourceError{Source: index + 1, Operation: "verify signature", Err: err})
				continue
			}
		}
		if err := repo.Storer.CheckAndSetReference(plumbing.NewHashReference(branch, candidate.Hash()), expected); err != nil {
			return err
		}
		logger.Infof("Fetched repository using source %d", index+1)
		return nil
	}
	return errors.Join(failures...)
}

func hasRepositoryMirrors(target *Target) bool { return len(repositoryURLs(target)) > 1 }

// Configured sources are the explicit trust list; validate transports before any
// credential is sent. Keep SSH usernames, but reject embedded HTTP credentials.
func validateRepositorySources(target *Target) error {
	credential := target.pat != "" || target.password != "" || (target.envSecret != "" && os.Getenv(target.envSecret) != "")
	for index, source := range repositoryURLs(target) {
		endpoint, err := transport.NewEndpoint(source)
		if err != nil || strings.TrimSpace(source) != source || source == "" {
			return &mirrorSourceError{Source: index + 1, Operation: "validate URL", Err: fmt.Errorf("invalid repository URL")}
		}
		switch endpoint.Protocol {
		case "https":
			if endpoint.User != "" || endpoint.Password != "" || endpoint.Host == "" {
				return &mirrorSourceError{Source: index + 1, Operation: "validate URL", Err: fmt.Errorf("HTTPS source must have a host and no embedded credentials")}
			}
		case "http":
			if credential || endpoint.User != "" || endpoint.Password != "" || endpoint.Host == "" {
				return &mirrorSourceError{Source: index + 1, Operation: "validate URL", Err: fmt.Errorf("HTTP sources cannot carry credentials")}
			}
		case "ssh":
			if endpoint.Host == "" || endpoint.Password != "" {
				return &mirrorSourceError{Source: index + 1, Operation: "validate URL", Err: fmt.Errorf("SSH source needs a host without embedded password")}
			}
		case "file":
			if !strings.HasPrefix(source, "file://") || !filepath.IsAbs(endpoint.Path) || (endpoint.Host != "" && endpoint.Host != "localhost") {
				return &mirrorSourceError{Source: index + 1, Operation: "validate URL", Err: fmt.Errorf("local sources require an absolute file:// URL")}
			}
		default:
			return &mirrorSourceError{Source: index + 1, Operation: "validate URL", Err: fmt.Errorf("unsupported repository transport")}
		}
	}
	return nil
}

type mirrorSourceError struct {
	Source    int
	Operation string
	Err       error
}

func (e *mirrorSourceError) Error() string {
	return fmt.Sprintf("repository source %d failed during %s", e.Source, e.Operation)
}
func (e *mirrorSourceError) Unwrap() error { return e.Err }

func mirrorCloneOptions(target *Target) (*git.CloneOptions, error) {
	auth := &githttp.BasicAuth{Username: target.username, Password: target.password}
	if target.pat != "" {
		auth.Username, auth.Password = "fetchit", target.pat
	}
	options := &git.CloneOptions{ReferenceName: plumbing.NewBranchReferenceName(target.branch), SingleBranch: true, Auth: auth}
	if target.ssh {
		key, err := ssh.NewPublicKeysFromFile("git", target.sshKey, target.password)
		if err != nil {
			return nil, err
		}
		// Fail closed using the configured SSH_KNOWN_HOSTS file or go-git's defaults.
		callback, err := ssh.NewKnownHostsCallback()
		if err != nil {
			return nil, err
		}
		key.HostKeyCallback = callback
		options.Auth = key
	}
	return options, nil
}

func verifyClone(ctx context.Context, repo *git.Repository, directory string, target *Target) error {
	if !target.gitsignVerify {
		return nil
	}
	head, err := repo.Head()
	if err != nil {
		return err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return err
	}
	return VerifyGitsign(ctx, commit, head.Hash().String()[:hashReportLen], directory, target.gitsignRekorURL)
}

func setPrimaryOrigin(repo *git.Repository, primary string) error {
	remote, err := repo.Remote("origin")
	if err != nil {
		return err
	}
	remoteConfig := remote.Config()
	remoteConfig.URLs = []string{primary}
	repoConfig, err := repo.Config()
	if err != nil {
		return err
	}
	repoConfig.Remotes["origin"] = remoteConfig
	return repo.SetConfig(repoConfig)
}

var repositoryCacheLocks sync.Map

// Different target configs can resolve to the same existing cache directory.
// Serialize branch updates and checkout together, not just the temporary ref.
func lockRepositoryCache(target *Target) func() {
	directory, err := filepath.Abs(getDirectory(target))
	if err != nil {
		directory = getDirectory(target)
	}
	value, _ := repositoryCacheLocks.LoadOrStore(directory, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func fetchCandidate(ctx context.Context, repo *git.Repository, source string, options *git.FetchOptions, incoming plumbing.ReferenceName) (*plumbing.Reference, error) {
	remote := git.NewRemote(repo.Storer, &config.RemoteConfig{Name: "fetchit-source", URLs: []string{source}})
	if err := remote.FetchContext(ctx, options); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, err
	}
	return repo.Reference(incoming, true)
}
