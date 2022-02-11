package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Repo struct {
	Url          string
	Directory    string
	Branch       string
	Method       string
	Subdirectory string
}

func main() {
	repoJson, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	var repo Repo
	json.Unmarshal([]byte(repoJson), &repo)

	if _, err := os.Stat(repo.Directory); os.IsNotExist(err) {
		fmt.Printf("git clone %s %s --recursive\n", repo.Url, repo.Branch)

		r, err := git.PlainClone(repo.Directory, false, &git.CloneOptions{
			URL:        repo.Url,
			RemoteName: repo.Branch,
		})
		ref, err := r.Head()

		// ... retrieving the commit object
		commit, err := r.CommitObject(ref.Hash())

		// ... retrieve the tree from the commit
		tree, err := commit.Tree()

		// ... get the files iterator and print the file
		tree.Files().ForEach(func(f *object.File) error {
			if strings.Contains(f.Name, repo.Subdirectory) {
				fmt.Println(f.Name)
			}
			return nil
		})

		if err != nil {
			log.Fatal(err)
		}
	} else if _, err := os.Stat(repo.Directory + "/.git"); !os.IsNotExist(err) {
		// Pull the latest changes from the remote
		fmt.Printf("Pulling latest repository changes from %s branch %s\n", repo.Url, repo.Branch)

		// Open the local repository
		r, err := git.PlainOpen(repo.Directory)
		if err != nil {
			log.Fatal(err)
		}

		w, err := r.Worktree()
		if err != nil {
			log.Fatal(err)
		}

		// Pull the latest changes from the origin remote and merge into the current branch
		err = w.Pull(&git.PullOptions{RemoteName: repo.Branch})
		if err != nil {
			log.Fatal("Nothing to pull.....Requeuing \n")
		}

		// Print the latest commit that was just pulled
		ref, err := r.Head()
		commit, err := r.CommitObject(ref.Hash())

		fmt.Println(commit)
	} else {
		fmt.Printf("%s exists but is not a git repository", repo.Directory)
	}
	fmt.Printf("Deploying items in %s format\n", repo.Method)
	if repo.Method == "raw" {
		path := repo.Directory + "/" + repo.Subdirectory
		rawPodman(path)
	}
}
