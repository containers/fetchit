package engine

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"k8s.io/klog/v2"
)

// downloadUpdateConfig returns true if config was updated in harpoon pod
func downloadUpdateConfigFile(urlStr string, existsAlready, initial bool) (bool, error) {
	_, err := url.Parse(urlStr)
	if err != nil {
		return false, fmt.Errorf("unable to parse config file url %s: %v", urlStr, err)
	}
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}
	resp, err := client.Get(urlStr)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	newBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("error downloading config from %s: %v", err)
	}
	if newBytes == nil {
		// if initial, this is the last resort, newBytes should be populated
		// the only way to get here from initial
		// is if there is no config file on disk, only a HARPOON_CONFIG_URL
		return false, fmt.Errorf("found empty config at %s, unable to update or populate config", urlStr)
	}
	if !initial {
		currentConfigBytes, err := ioutil.ReadFile(defaultConfigPath)
		if err != nil {
			klog.Infof("unable to read current config, will try with new downloaded config file: %v", err)
			existsAlready = false
		} else {
			if bytes.Equal(newBytes, currentConfigBytes) {
				return false, nil
			}
		}

		if existsAlready {
			if err := os.WriteFile(defaultConfigBackup, currentConfigBytes, 0600); err != nil {
				return false, fmt.Errorf("could not copy %s to path %s: %v", defaultConfigPath, defaultConfigBackup, err)
			}
			klog.Infof("Current config backup placed at %s", defaultConfigBackup)
		}
	}
	if err := os.WriteFile(defaultConfigPath, newBytes, 0600); err != nil {
		return false, fmt.Errorf("unable to write new config contents, reverting to old config: %v", err)
	}

	klog.Infof("Config updates found from url: %s, will load new targets", urlStr)
	return true, nil
}

func getChangeString(change *object.Change) (*string, error) {
	if change != nil {
		_, to, err := change.Files()
		if err != nil {
			return nil, err
		}
		if to != nil {
			s, err := to.Contents()
			if err != nil {
				return nil, err
			}
			return &s, nil
		}
	}
	return nil, nil
}

func checkTag(tags *[]string, name string) bool {
	if tags == nil {
		return true
	}
	for _, tag := range *tags {
		if strings.HasSuffix(name, tag) {
			return true
		}
	}
	return false
}

func getTree(r *git.Repository, oldCommit *object.Commit) (*object.Tree, *object.Commit, error) {
	if oldCommit != nil {
		// ... retrieve the tree from the commit
		tree, err := oldCommit.Tree()
		if err != nil {
			return nil, nil, fmt.Errorf("error when retrieving tree: %s", err)
		}
		return tree, nil, nil
	}
	var newCommit *object.Commit
	ref, err := r.Head()
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving head: %s", err)
	}
	// ... retrieving the commit object
	newCommit, err = r.CommitObject(ref.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving commit: %s", err)
	}

	// ... retrieve the tree from the commit
	tree, err := newCommit.Tree()
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving tree: %s", err)
	}
	return tree, newCommit, nil
}
