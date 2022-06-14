package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func applyChanges(ctx context.Context, target *Target, targetPath string, currentState, desiredState plumbing.Hash, tags *[]string) (map[*object.Change]string, error) {
	if desiredState.IsZero() {
		return nil, errors.New("Cannot run Apply if desired state is empty")
	}
	trimDir := strings.TrimSuffix(target.url, path.Ext(target.url))
	directory := filepath.Base(trimDir)

	currentTree, err := getTreeFromHash(directory, currentState)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting tree from hash %s", currentState)
	}

	desiredTree, err := getTreeFromHash(directory, desiredState)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting tree from hash %s", desiredState)
	}

	changeMap, err := getFilteredChangeMap(directory, targetPath, currentTree, desiredTree, tags)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting filtered change map from %s to %s", currentState, desiredState)
	}

	return changeMap, nil
}

//getLatest will get the head of the branch in the repository specified by the target's url
func getLatest(target *Target) (plumbing.Hash, error) {
	trimDir := strings.TrimSuffix(target.url, path.Ext(target.url))
	directory := filepath.Base(trimDir)
	repo, err := git.PlainOpen(directory)
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error opening repository: %s", directory)
	}

	refSpec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", target.branch, target.branch))
	if err = repo.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{refSpec, "HEAD:refs/heads/HEAD"},
		Force:    true,
	}); err != nil && err != git.NoErrAlreadyUpToDate && !target.disconnected {
		return plumbing.Hash{}, utils.WrapErr(err, "Error fetching branch %s from remote repository %s", target.branch, target.url)
	}

	branch, err := repo.Reference(plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", target.branch)), false)
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error getting reference to branch %s", target.branch)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error getting reference to worktree for repository", target.Name)
	}

	err = wt.Checkout(&git.CheckoutOptions{Hash: branch.Hash()})
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error checking out %s on branch %s", branch.Hash(), target.branch)
	}

	return branch.Hash(), err
}

func getCurrent(target *Target, methodType, methodName string) (plumbing.Hash, error) {
	trimDir := strings.TrimSuffix(target.url, path.Ext(target.url))
	directory := filepath.Base(trimDir)
	tagName := fmt.Sprintf("current-%s-%s", methodType, methodName)

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error opening repository: %s", directory)
	}

	ref, err := repo.Tag(tagName)
	if err == git.ErrTagNotFound {
		return plumbing.Hash{}, nil
	} else if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error getting reference to current tag")
	}

	return ref.Hash(), err
}

func updateCurrent(ctx context.Context, target *Target, newCurrent plumbing.Hash, methodType, methodName string) error {
	trimDir := strings.TrimSuffix(target.url, path.Ext(target.url))
	directory := filepath.Base(trimDir)
	tagName := fmt.Sprintf("current-%s-%s", methodType, methodName)

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return utils.WrapErr(err, "Error opening repository: %s", directory)
	}

	err = repo.DeleteTag(tagName)
	if err != nil && err != git.ErrTagNotFound {
		return utils.WrapErr(err, "Error deleting old current tag")
	}

	_, err = repo.CreateTag(tagName, newCurrent, nil)
	if err != nil {
		return utils.WrapErr(err, "Error creating new current tag with hash %s", newCurrent)
	}

	return nil
}

func getTreeFromHash(directory string, hash plumbing.Hash) (*object.Tree, error) {
	if hash.IsZero() {
		return &object.Tree{}, nil
	}

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return nil, utils.WrapErr(err, "Error opening repository: %s", directory)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting commit at hash %s from repo %s", hash, directory)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting tree from commit at hash %s from repo %s", hash, directory)
	}

	return tree, nil
}

func getFilteredChangeMap(
	directory string,
	targetPath string,
	currentTree,
	desiredTree *object.Tree,
	tags *[]string,
) (map[*object.Change]string, error) {

	changes, err := currentTree.Diff(desiredTree)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting diff between current and latest", targetPath)
	}

	changeMap := make(map[*object.Change]string)
	for _, change := range changes {
		if strings.Contains(change.To.Name, targetPath) {
			checkTag(tags, change.To.Name)
			path := filepath.Join(directory, change.To.Name)
			changeMap[change] = path
		} else if strings.Contains(change.From.Name, targetPath) {
			checkTag(tags, change.From.Name)
			changeMap[change] = deleteFile
		}
	}

	return changeMap, nil
}

func checkTag(tags *[]string, name string) bool {
	if tags == nil {
		return true
	}
	for _, suffix := range *tags {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func getChangeString(change *object.Change) (*string, error) {
	if change != nil {
		from, _, err := change.Files()
		if err != nil {
			return nil, err
		}
		if from != nil {
			s, err := from.Contents()
			if err != nil {
				return nil, err
			}
			return &s, nil
		}
	}
	return nil, nil
}
