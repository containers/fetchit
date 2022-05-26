package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

type GitManager struct {
	repos map[string]TargetRepo
}

type TargetRepo struct {
	*git.Repository
	methodMap map[string]plumbing.Hash
}

func newGitManager() *GitManager {
	return &GitManager{
		repos: make(map[string]TargetRepo),
	}
}

func (gm *GitManager) AddTarget(targetName, url, branch, authToken string) error {
	absPath, err := filepath.Abs(targetName)
	if err != nil {
		return err
	}

	exists, err := checkPath(absPath)
	if err != nil {
		return err
	}

	if !exists {
		repo, err := cloneRepo(absPath, authToken, url, branch)
		if err != nil {
			return err
		}
		if _, ok := gm.repos[targetName]; !ok {
			gm.repos[targetName] = TargetRepo{repo, make(map[string]plumbing.Hash)}
		}
	} else {
		repo, err := git.PlainOpen(absPath)
		if err != nil {
			return err
		}
		if _, ok := gm.repos[targetName]; !ok {
			gm.repos[targetName] = TargetRepo{repo, make(map[string]plumbing.Hash)}
		}
	}
	return nil
}

func cloneRepo(path, authToken, url, branch string) (*git.Repository, error) {
	var user string
	if authToken != "" {
		user = "fetchit"
	}

	repo, err := git.PlainClone(path, false, &git.CloneOptions{
		Auth: &http.BasicAuth{
			Username: user, // the value of this field should not matter when using a PAT
			Password: authToken,
		},
		URL:           url,
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branch)),
		SingleBranch:  true,
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func checkPath(path string) (bool, error) {
	var exists bool
	if _, err := os.Stat(path); err == nil {
		exists = true
		// if directory/.git does not exist, fail quickly
		if _, err := os.Stat(path + "/.git"); err != nil {
			return false, fmt.Errorf("%s exists but is not a git repository", path)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return exists, nil
}

func (gm *GitManager) GetLatestCommit(targetName string) (plumbing.Hash, error) {
	gm.repos[targetName].Fetch(&git.FetchOptions{})
	ref, err := gm.repos[targetName].Head()
	if err != nil {
		return plumbing.Hash{}, err
	}

	hash := ref.Hash()
	return hash, nil
}

func (gm *GitManager) GetCommit(targetName string, hash plumbing.Hash) (*object.Commit, error) {
	commit, err := gm.repos[targetName].CommitObject(hash)
	if err != nil {
		return nil, err
	}
	return commit, nil
}

func (gm *GitManager) GetDiff(targetName string, hashBefore, hashAfter plumbing.Hash) (*object.Changes, error) {
	beforeCommit, err := gm.repos[targetName].CommitObject(hashBefore)
	if err != nil {
		return &object.Changes{}, err
	}
	afterCommit, err := gm.repos[targetName].CommitObject(hashAfter)
	if err != nil {
		return &object.Changes{}, err
	}

	beforeTree, err := beforeCommit.Tree()
	if err != nil {
		return &object.Changes{}, err
	}
	afterTree, err := afterCommit.Tree()
	if err != nil {
		return &object.Changes{}, err
	}

	changes, err := afterTree.Diff(beforeTree)
	if err != nil {
		return &object.Changes{}, err
	}

	return &changes, nil
}

func (gm *GitManager) GetCurrentWorkingCommit(targetName, method string) (plumbing.Hash, error) {
	if hash, ok := gm.repos[targetName].methodMap[method]; ok {
		return hash, nil
	} else {
		return plumbing.Hash{}, fmt.Errorf("Unable to get working commit: does not exist for method %s, for target %s", method, targetName)
	}
}

func (gm *GitManager) SetCurrentWorkingCommit(targetName, method string, hash plumbing.Hash) {
	gm.repos[targetName].methodMap[method] = hash
}
