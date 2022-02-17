package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-co-op/gocron"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/redhat-et/harpoon/pkg/engine"
)

type Repo struct {
	Url          string
	PAT          string
	Branch       string
	Method       string
	Subdirectory string
	Schedule     string
	firstRun     bool
}

func main() {
	repoJson, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Error while reading config file: %s\n", err)
	}
	var repo Repo
	json.Unmarshal([]byte(repoJson), &repo)

	repo.firstRun = true
	s := gocron.NewScheduler(time.UTC)
	s.Cron(repo.Schedule).Do(repo.process)
	s.StartAsync()
	select {}
}

func (repo *Repo) process() {
	directory := filepath.Base(repo.Url)
	var exists bool
	if _, err := os.Stat(directory); err == nil {
		exists = true
		// if directory/.git does not exist, fail quickly
		if _, err := os.Stat(directory + "/.git"); os.IsNotExist(err) {
			log.Fatalf("%s exists but is not a git repository", directory)
		}
	} else if !os.IsNotExist(err) {
		log.Fatalf("Error when retrieving repository: %s\n", err)
	}

	if !exists {
		var user string
		if repo.PAT != "" {
			user = "harpoon"
		}
		fmt.Printf("git clone %s %s --recursive\n", repo.Url, repo.Branch)
		_, err := git.PlainClone(directory, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user,
				Password: repo.PAT,
			},
			URL:           repo.Url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", repo.Branch)),
			SingleBranch:  true,
		})
		if err != nil {
			log.Fatalf("Error while cloning the repository: %s\n", err)
		}
	}
	// Open the local repository
	r, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Error while opening the repository: %s\n", err)
	}

	tree, err := getTree(r)
	if err != nil {
		log.Fatal(err)
	}
	if repo.firstRun {
		repo.firstRun = false
		// ... get subdirectory tree
		subDirTree, err := tree.Tree(repo.Subdirectory)
		if err != nil {
			log.Fatalf("Error when switching to subdirectory tree: %s\n", err)
		}

		// ... get the files iterator and print the file
		// .. make sure we're only calling the engine method on json files
		subDirTree.Files().ForEach(func(f *object.File) error {
			if strings.HasSuffix(f.Name, ".json") || strings.HasSuffix(f.Name, ".service") || strings.HasSuffix(f.Name, ".yaml") || strings.HasSuffix(f.Name, ".yml") {
				path := directory + "/" + repo.Subdirectory + "/" + f.Name
				if err := engine.EngineMethod(path, repo.Method); err != nil {
					log.Fatal(err)
				}
			}
			return nil
		})
	}
	// Pull the latest changes from the remote
	fmt.Printf("Pulling latest repository changes from %s branch %s\n", repo.Url, repo.Branch)

	w, err := r.Worktree()
	if err != nil {
		log.Fatalf("Error while opening the worktree: %s\n", err)
	}

	// ... retrieve the tree from the commit
	prevTree, err := getTree(r)
	if err != nil {
		log.Fatal(err)
	}

	// Pull the latest changes from the origin remote and merge into the current branch
	if err = w.Pull(&git.PullOptions{
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", repo.Branch)),
		SingleBranch:  true,
	}); err != nil && !repo.firstRun {
		fmt.Println("Nothing to pull.....Requeuing \n")
		return
	}

	tree, err = getTree(r)
	if err != nil {
		log.Fatal(err)
	}

	changes, err := tree.Diff(prevTree)
	if err != nil {
		log.Fatalf("Error while generating diff: %s\n", err)
	}
	for _, change := range changes {
		if strings.Contains(change.To.Name, repo.Subdirectory) {
			path := directory + "/" + change.To.Name
			if err := engine.EngineMethod(path, repo.Method); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func getTree(r *git.Repository) (*object.Tree, error) {
	ref, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving head: %s\n", err)
	}

	// ... retrieving the commit object
	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving commit: %s\n", err)
	}

	// ... retrieve the tree from the commit
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving tree: %s\n", err)
	}
	return tree, nil
}
