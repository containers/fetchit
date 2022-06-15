package engine

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"k8s.io/klog/v2"
)

type CommonMethod struct {
	// Name must be unique within target method
	Name string `mapstructure:"name"`
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// A glob to pattern match files in the target path directory
	Glob *string `mapstructure:"glob"`
	// initialRun is set by fetchit
	initialRun bool
	target     *Target
}

func (m *CommonMethod) GetName() string {
	return m.Name
}

func (m *CommonMethod) SchedInfo() SchedInfo {
	return SchedInfo{
		schedule: m.Schedule,
		skew:     m.Skew,
	}
}

func (m *CommonMethod) GetTargetPath() string {
	return m.TargetPath
}

func (m *CommonMethod) GetTarget() *Target {
	return m.target
}

func zeroToCurrent(ctx, conn context.Context, m Method, target *Target, tag *[]string) error {
	current, err := getCurrent(target, m.GetKind(), m.GetName())
	if err != nil {
		return fmt.Errorf("Failed to get current commit: %v", err)
	}

	if current != plumbing.ZeroHash {
		err = m.Apply(ctx, conn, plumbing.ZeroHash, current, tag)
		if err != nil {
			return fmt.Errorf("Failed to apply changes: %v", err)
		}

		klog.Infof("Moved %s to %s for target %s", m.GetName(), current, target.name)
	}

	return nil
}

func extractZip(url, name string) error {
	trimDir := strings.TrimSuffix(url, path.Ext(url))
	directory := filepath.Base(trimDir)
	absPath, err := filepath.Abs(directory)

	klog.Infof("loading disconnected archive from %s", url)
	// Place the data into the placeholder file
	data, err := http.Get(url)
	if err != nil {
		klog.Error("Failed getting data from ", url)
		return err
	}
	defer data.Body.Close()

	// Fail early if http error code is not 200
	if data.StatusCode != http.StatusOK {
		klog.Error("Failed getting data from ", url)
		return err
	}

	// Unzip the data from the http response
	// Create the destination file
	os.MkdirAll(directory, 0755)

	outFile, err := os.Create(absPath + "/" + name + ".zip")
	if err != nil {
		klog.Error("Failed creating file ", absPath+"/"+name+".zip")
		return err
	}

	// Write the body to file
	io.Copy(outFile, data.Body)

	// Unzip the file
	r, err := zip.OpenReader(outFile.Name())
	if err != nil {
		klog.Infof("error opening zip file: %s", err)
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		fpath := filepath.Join(directory, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
		} else {
			var fdir string
			if lastIndex := strings.LastIndex(fpath, string(os.PathSeparator)); lastIndex > -1 {
				fdir = fpath[:lastIndex]
			}

			os.MkdirAll(fdir, f.Mode())
			f, err := os.OpenFile(
				fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
	}
	err = os.Remove(outFile.Name())
	if err != nil {
		klog.Error("Failed removing file ", outFile.Name())
		return err
	}
	return nil
}

func currentToLatest(ctx, conn context.Context, m Method, target *Target, tag *[]string) error {
	if target.disconnected {
		extractZip(target.url, target.name)
	}
	latest, err := getLatest(target)
	if err != nil {
		return fmt.Errorf("Failed to get latest commit: %v", err)
	}

	current, err := getCurrent(target, m.GetKind(), m.GetName())
	if err != nil {
		return fmt.Errorf("Failed to get current commit: %v", err)
	}

	if latest != current {
		err = m.Apply(ctx, conn, current, latest, tag)
		if err != nil {
			return fmt.Errorf("Failed to apply changes: %v", err)
		}

		updateCurrent(ctx, target, latest, m.GetKind(), m.GetName())
		klog.Infof("Moved %s from %s to %s for target %s", m.GetName(), current, latest, target.name)
	} else {
		klog.Infof("No changes applied to target %s this run, %s currently at %s", target.name, m.GetKind(), current)
	}

	return nil
}

func runChangesConcurrent(ctx context.Context, conn context.Context, m Method, changeMap map[*object.Change]string) error {
	ch := make(chan error)
	for change, changePath := range changeMap {
		go func(ch chan<- error, changePath string, change *object.Change) {
			if err := m.MethodEngine(ctx, conn, change, changePath); err != nil {
				ch <- utils.WrapErr(err, "error running engine method for change from: %s to %s", change.From.Name, change.To.Name)
			}
			ch <- nil
		}(ch, changePath, change)
	}
	for range changeMap {
		err := <-ch
		if err != nil {
			return err
		}
	}
	return nil
}
