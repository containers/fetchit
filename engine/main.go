package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-co-op/gocron"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Repo struct {
	Url          string
	Branch       string
	Method       string
	Subdirectory string
	Schedule     string
}

func main() {
	repoJson, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	var repo Repo
	json.Unmarshal([]byte(repoJson), &repo)

	s := gocron.NewScheduler(time.UTC)
	s.Cron(repo.Schedule).Do(process)
	s.StartAsync()
	select {}
}

func process() {
	repoJson, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	var repo Repo
	json.Unmarshal([]byte(repoJson), &repo)

	directory := repo.Url[strings.LastIndex(repo.Url, "/")+1:]

	if _, err := os.Stat(directory); os.IsNotExist(err) {
		fmt.Printf("git clone %s %s --recursive\n", repo.Url, repo.Branch)

		r, err := git.PlainClone(directory, false, &git.CloneOptions{
			URL:           repo.Url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", repo.Branch)),
			SingleBranch:  true,
		})
		if err != nil {
			fmt.Printf("Error while cloning the repository: %s\n", err)
		}
		ref, err := r.Head()
		if err != nil {
			fmt.Printf("Error when retrieving head: %s\n", err)
		}

		// ... retrieving the commit object
		commit, err := r.CommitObject(ref.Hash())
		if err != nil {
			fmt.Printf("Error when retrieving commit: %s\n", err)
		}

		// ... retrieve the tree from the commit
		tree, err := commit.Tree()

		// ... get the files iterator and print the file
		tree.Files().ForEach(func(f *object.File) error {
			if strings.Contains(f.Name, repo.Subdirectory) {
				path := directory + "/" + f.Name
				engineMethod(path, repo.Method)
			}
			return nil
		})

		if err != nil {
			log.Fatal(err)
		}
	} else if _, err := os.Stat(directory + "/.git"); !os.IsNotExist(err) {
		// Pull the latest changes from the remote
		fmt.Printf("Pulling latest repository changes from %s branch %s\n", repo.Url, repo.Branch)

		// Open the local repository
		r, err := git.PlainOpen(directory)
		if err != nil {
			log.Fatal(err)
		}

		w, err := r.Worktree()
		if err != nil {
			log.Fatal(err)
		}

		ref, err := r.Head()
		if err != nil {
			fmt.Printf("Error when retrieving head: %s\n", err)
		}

		// ... retrieving the commit object
		prevCommit, err := r.CommitObject(ref.Hash())
		if err != nil {
			fmt.Printf("Error when retrieving commit: %s\n", err)
		}

		// ... retrieve the tree from the commit
		prevTree, err := prevCommit.Tree()
		if err != nil {
			fmt.Printf("Error while generating tree: %s\n", err)
		}

		// Pull the latest changes from the origin remote and merge into the current branch
		err = w.Pull(&git.PullOptions{
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", repo.Branch)),
			SingleBranch:  true,
		})
		if err != nil {
			fmt.Printf("Nothing to pull.....Requeuing \n")
		} else {
			// Print the latest commit that was just pulled
			ref, err := r.Head()
			if err != nil {
				fmt.Printf("An error has occured %s\n", err)
			}
			commit, err := r.CommitObject(ref.Hash())
			if err != nil {
				fmt.Printf("Error when retrieving commit: %s\n", err)
			}

			// ... retrieve the tree from the commit
			tree, err := commit.Tree()
			if err != nil {
				fmt.Printf("Error while generating tree: %s\n", err)
			}

			changes, err := tree.Diff(prevTree)
			if err != nil {
				log.Fatal(err)
			}
			for _, change := range changes {
				if strings.Contains(change.To.Name, repo.Subdirectory) {
					path := directory + "/" + change.To.Name
					engineMethod(path, repo.Method)
				}
			}
		}
	} else {
		fmt.Printf("%s exists but is not a git repository", repo.Url)
	}
}

func engineMethod(path string, method string) {
	if method == "raw" {
		rawPodman(path)
	}
	if method == "kube" {
		fmt.Printf("TBD")
	}
}
