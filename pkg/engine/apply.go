package engine

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gobwas/glob"
	gitsign "github.com/sigstore/gitsign/pkg/git"
	gitsignrekor "github.com/sigstore/gitsign/pkg/rekor"
	rekorclient "github.com/sigstore/rekor/pkg/client"
)

const (
	defaultRekorURL = "https://rekor.sigstore.dev"
	hashReportLen   = 9
)

func applyChanges(ctx context.Context, target *Target, targetPath string, globPattern *string, currentState, desiredState plumbing.Hash, tags *[]string) (map[*object.Change]string, error) {
	if desiredState.IsZero() {
		return nil, errors.New("Cannot run Apply if desired state is empty")
	}
	directory := getDirectory(target)

	currentTree, err := getSubTreeFromHash(directory, currentState, targetPath)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting tree from hash %s", currentState)
	}

	desiredTree, err := getSubTreeFromHash(directory, desiredState, targetPath)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting tree from hash %s", desiredState)
	}

	changeMap, err := getFilteredChangeMap(directory, targetPath, globPattern, currentTree, desiredTree, tags)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting filtered change map from %s to %s", currentState, desiredState)
	}

	return changeMap, nil
}

//getLatest will get the head of the branch in the repository specified by the target's url
func getLatest(target *Target) (plumbing.Hash, error) {
	ctx := context.Background()
	directory := getDirectory(target)

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error opening repository %s to fetch latest commit", directory)
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
		return plumbing.Hash{}, utils.WrapErr(err, "Error getting reference to worktree for repository", directory)
	}

	hashStr := branch.Hash().String()[:hashReportLen]
	if err := wt.Checkout(&git.CheckoutOptions{Hash: branch.Hash()}); err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error checking out %s on branch %s", hashStr, target.branch)
	}

	if target.gitsignVerify {
		commit, err := repo.CommitObject(branch.Hash())
		if err != nil {
			return plumbing.Hash{}, utils.WrapErr(err, "Error getting verified commit at hash %s from repository %s", hashStr, directory)
		}
		if err := VerifyGitsign(ctx, commit, hashStr, directory, target.gitsignRekorURL); err != nil {
			return plumbing.Hash{}, utils.WrapErr(err, "Requested verified commit signatures, but commit %s from repository %s failed verification", hashStr, directory)
		}
	}
	return branch.Hash(), err
}

// VerifyGitsign verifies any commit signed using sigstore/gitsign & rekor
func VerifyGitsign(ctx context.Context, commit *object.Commit, hash, repo, url string) error {
	if commit.PGPSignature == "" {
		return fmt.Errorf("Requested verified commit signatures, but commit %s from repository %s has no PGPSignature", hash, repo)
	}
	// Extract signature from commit
	pgpsig := commit.PGPSignature + "\n"
	r := strings.NewReader(pgpsig)
	sig := make([]byte, len(pgpsig))
	if _, err := r.Read(sig); err != nil {
		return utils.WrapErr(err, "Error reading signature from commit %s", hash)
	}
	// Extract everything else from commit
	d := &plumbing.MemoryObject{}
	if err := commit.EncodeWithoutSignature(d); err != nil {
		return utils.WrapErr(err, "Error decoding data from commit %s", hash)
	}
	er, err := d.Reader()
	if err != nil {
		return utils.WrapErr(err, "Error configuring data reader from commit %s", hash)
	}
	data := make([]byte, d.Size())
	if _, err = er.Read(data); err != nil {
		return utils.WrapErr(err, "Error reading data from commit %s", hash)
	}

	// Rekor client
	rekorURL := url
	if rekorURL == "" {
		rekorURL = defaultRekorURL
	}
	client, err := gitsignrekor.New(rekorURL, rekorclient.WithUserAgent("gitsign"))
	if err != nil {
		return utils.WrapErr(err, "Error obtaining rekor client")
	}
	summary, err := gitsign.Verify(ctx, client, data, sig, true)
	if err != nil {
		if summary != nil && summary.Cert != nil {
			logger.Infof("Bad Signature: GNUPG: %s %s", certHexFingerprint(summary.Cert), summary.Cert.Subject.String())
		}
		return utils.WrapErr(err, "Failed to verify signature")
	}
	logger.Infof("Validated Git signature: GNUPG: %s SUBJECT/ISSUER: %s %s", certHexFingerprint(summary.Cert), summary.Cert.Subject.String(), summary.Cert.Issuer)
	logger.Infof("Validated Rekor entry: %d From: %s", summary.LogEntry.LogIndex, summary.Cert.EmailAddresses)
	return nil
}

// borrowed from sigstore/gitsign/internal/git
// certHexFingerprint calculates the hex SHA1 fingerprint of a certificate.
func certHexFingerprint(cert *x509.Certificate) string {
	if len(cert.Raw) == 0 {
		return ""
	}

	fpr := sha1.Sum(cert.Raw)
	return hex.EncodeToString(fpr[:])
}

func checkout(target *Target, hash plumbing.Hash) error {
	if hash == plumbing.ZeroHash {
		return nil
	}

	directory := getDirectory(target)

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return utils.WrapErr(err, "Error opening repository: %s", directory)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return utils.WrapErr(err, "Error getting reference to worktree for repository", directory)
	}

	err = wt.Checkout(&git.CheckoutOptions{Hash: hash})
	if err != nil {
		return utils.WrapErr(err, "Error checking out %s on branch %s", hash, target.branch)
	}

	return nil
}

func getCurrent(target *Target, methodType, methodName string) (plumbing.Hash, error) {
	directory := getDirectory(target)
	tagName := fmt.Sprintf("current-%s-%s", methodType, methodName)

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return plumbing.Hash{}, utils.WrapErr(err, "Error opening repository %s to fetch current commit", directory)
	}

	ref, err := repo.Tag(tagName)
	if err != nil {
		if err == git.ErrTagNotFound {
			return plumbing.Hash{}, nil
		}
		return plumbing.Hash{}, utils.WrapErr(err, "Error getting reference to current tag")
	}

	return ref.Hash(), err
}

func updateCurrent(ctx context.Context, target *Target, newCurrent plumbing.Hash, methodType, methodName string) error {
	directory := getDirectory(target)
	tagName := fmt.Sprintf("current-%s-%s", methodType, methodName)

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return utils.WrapErr(err, "Error opening repository %s to update current commit", directory)
	}

	err = repo.DeleteTag(tagName)
	if err != nil && err != git.ErrTagNotFound {
		return utils.WrapErr(err, "Error deleting old current tag")
	}

	if _, err := repo.CreateTag(tagName, newCurrent, nil); err != nil {
		return utils.WrapErr(err, "Error creating new current tag with hash %s", newCurrent)
	}

	return nil
}

func getSubTreeFromHash(directory string, hash plumbing.Hash, targetPath string) (*object.Tree, error) {
	if hash.IsZero() {
		return &object.Tree{}, nil
	}

	repo, err := git.PlainOpen(directory)
	if err != nil {
		return nil, utils.WrapErr(err, "Error opening repository %s to fetch sub tree from commit", directory)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting commit at hash %s from repository %s", hash, directory)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting tree from commit at hash %s from repository %s", hash, directory)
	}

	subTree, err := tree.Tree(targetPath)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting sub tree at %s from commit at %s from repository %s", targetPath, hash, directory)
	}

	return subTree, nil
}

func getFilteredChangeMap(
	directory,
	targetPath string,
	globPattern *string,
	currentTree,
	desiredTree *object.Tree,
	tags *[]string,
) (map[*object.Change]string, error) {

	changes, err := currentTree.Diff(desiredTree)
	if err != nil {
		return nil, utils.WrapErr(err, "Error getting diff between current and latest", targetPath)
	}

	var g glob.Glob
	if globPattern == nil {
		g, err = glob.Compile("**")
		if err != nil {
			return nil, utils.WrapErr(err, "Error compiling glob for pattern %s", globPattern)
		}
	} else {
		g, err = glob.Compile(*globPattern)
		if err != nil {
			return nil, utils.WrapErr(err, "Error compiling glob for pattern %s", globPattern)
		}
	}

	changeMap := make(map[*object.Change]string)
	for _, change := range changes {
		if change.To.Name != "" && checkTag(tags, change.To.Name) && g.Match(change.To.Name) {
			path := filepath.Join(directory, targetPath, change.To.Name)
			changeMap[change] = path
		} else if change.From.Name != "" && checkTag(tags, change.From.Name) && g.Match(change.From.Name) {
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
