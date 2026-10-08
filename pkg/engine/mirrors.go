package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func repositoryURLs(target *Target) []string {
	urls := []string{target.url}
	seen := map[string]bool{target.url: true}
	for _, url := range target.fallbackURLs {
		if url != "" && !seen[url] {
			urls = append(urls, url)
			seen[url] = true
		}
	}
	return urls
}

// Clone each candidate in a separate staging directory so a failed clone never
// leaves a partial repository that prevents the next candidate or later retry.
func getCloneWithMirrors(target *Target) error {
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
	auth := &githttp.BasicAuth{Username: target.username, Password: target.password}
	if target.pat != "" {
		auth.Username, auth.Password = "fetchit", target.pat
	}
	options := &git.CloneOptions{ReferenceName: plumbing.NewBranchReferenceName(target.branch), SingleBranch: true, Auth: auth}
	if target.ssh {
		key, err := ssh.NewPublicKeysFromFile("git", target.sshKey, target.password)
		if err != nil {
			return err
		}
		options.Auth = key
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
		cancel()
		if cloneErr == nil && target.gitsignVerify {
			head, err := repo.Head()
			if err != nil {
				cloneErr = err
			} else {
				commit, err := repo.CommitObject(head.Hash())
				if err != nil {
					cloneErr = err
				} else {
					cloneErr = VerifyGitsign(context.Background(), commit, head.Hash().String()[:hashReportLen], stage, target.gitsignRekorURL)
				}
			}
		}
		if cloneErr == nil {
			// Keep origin stable even if the initial clone came from a mirror.
			remote, remoteErr := repo.Remote("origin")
			if remoteErr != nil {
				cloneErr = remoteErr
			} else {
				remoteConfig := remote.Config()
				remoteConfig.URLs = []string{target.url}
				repoConfig, configErr := repo.Config()
				if configErr != nil {
					cloneErr = configErr
				} else {
					repoConfig.Remotes["origin"] = remoteConfig
					cloneErr = repo.SetConfig(repoConfig)
				}
			}
		}
		if cloneErr == nil {
			cloneErr = os.Rename(stage, destination)
		}
		if cloneErr == nil {
			logger.Infof("Cloned repository using source %d", index+1)
			return nil
		}
		_ = os.RemoveAll(stage)
		// Do not include URLs or transport error text: either can contain credentials.
		failures = append(failures, fmt.Errorf("repository source %d failed to clone", index+1))
	}
	return errors.Join(failures...)
}

// Fetch into a temporary ref. Rejected or failed mirrors never move the tracked
// branch. Primary changes retain the existing force-fetch behavior.
func fetchWithMirrors(repo *git.Repository, target *Target, original *git.FetchOptions) error {
	branch := plumbing.NewBranchReferenceName(target.branch)
	incoming := plumbing.ReferenceName("refs/fetchit/incoming/" + target.branch)
	defer repo.Storer.RemoveReference(incoming)
	var failures []error
	for index, url := range repositoryURLs(target) {
		if err := repo.Storer.RemoveReference(incoming); err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return err
		}
		options := *original
		options.RefSpecs = []config.RefSpec{config.RefSpec("+" + branch.String() + ":" + incoming.String())}
		options.RemoteName = "fetchit-source"
		// Applied-state tags are local bookkeeping; mirrors must not import tags.
		options.Tags = git.NoTags
		remote := git.NewRemote(repo.Storer, &config.RemoteConfig{Name: "fetchit-source", URLs: []string{url}})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := remote.FetchContext(ctx, &options)
		cancel()
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			failures = append(failures, fmt.Errorf("repository source %d failed to fetch", index+1))
			continue
		}
		candidate, err := repo.Reference(incoming, true)
		if err != nil {
			failures = append(failures, fmt.Errorf("repository source %d has no requested branch", index+1))
			continue
		}
		if index > 0 {
			current, err := repo.Reference(branch, true)
			if err != nil {
				return err
			}
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
			if err := VerifyGitsign(context.Background(), commit, candidate.Hash().String()[:hashReportLen], getDirectory(target), target.gitsignRekorURL); err != nil {
				failures = append(failures, fmt.Errorf("repository source %d failed signature verification", index+1))
				continue
			}
		}
		if err := repo.Storer.SetReference(plumbing.NewHashReference(branch, candidate.Hash())); err != nil {
			return err
		}
		logger.Infof("Fetched repository using source %d", index+1)
		return nil
	}
	return errors.Join(failures...)
}
