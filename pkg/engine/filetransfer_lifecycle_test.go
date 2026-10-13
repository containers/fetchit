package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFileTransferCreateRenameDeleteAndRetry(t *testing.T) {
	old := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = old })
	for _, tc := range []struct {
		name, targetPath, from, to string
		fail                       bool
		want                       [][]string
	}{
		{name: "create single file", to: "new file.txt", want: [][]string{{"rsync", "-avz", "--mkpath", "--", "/opt/repo/new file.txt", "/host/dest/new file.txt"}}},
		{name: "create nested file", targetPath: "source", to: "nested/deep/new file.txt", want: [][]string{{"rsync", "-avz", "--mkpath", "--", "/opt/repo/source/nested/deep/new file.txt", "/host/dest/nested/deep/new file.txt"}}},
		{name: "rename nested file", targetPath: "source", from: "nested/old file.txt", to: "nested/deep/new file.txt", want: [][]string{{"rm", "-f", "--", "/host/dest/nested/old file.txt"}, {"rsync", "-avz", "--mkpath", "--", "/opt/repo/source/nested/deep/new file.txt", "/host/dest/nested/deep/new file.txt"}}},
		{name: "delete nested file", targetPath: "source", from: "nested/old file.txt", want: [][]string{{"rm", "-f", "--", "/host/dest/nested/old file.txt"}}},
		{name: "failed nested removal", targetPath: "source", from: "nested/old file.txt", to: "nested/deep/new file.txt", fail: true, want: [][]string{{"rm", "-f", "--", "/host/dest/nested/old file.txt"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands [][]string
			conn := testPodmanConnection(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/create"):
					var spec struct {
						Command []string `json:"command"`
					}
					if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
						t.Error(err)
					}
					commands = append(commands, spec.Command)
					fmt.Fprint(w, `{"Id":"copy"}`)
				case strings.HasSuffix(r.URL.Path, "/start"):
					w.WriteHeader(204)
				case strings.HasSuffix(r.URL.Path, "/wait"):
					if tc.fail {
						fmt.Fprint(w, "1")
					} else {
						fmt.Fprint(w, "0")
					}
				case r.Method == http.MethodDelete:
					fmt.Fprint(w, "[]")
				default:
					t.Errorf("unexpected API call %s %s", r.Method, r.URL.Path)
				}
			})
			change := &object.Change{From: object.ChangeEntry{Name: tc.from}, To: object.ChangeEntry{Name: tc.to}}
			path := filepath.Join("repo", tc.targetPath, tc.to)
			if tc.to == "" {
				path = deleteFile
			}
			method := &FileTransfer{
				DestinationDirectory: "/host/dest",
				CommonMethod:         CommonMethod{Name: "copy", TargetPath: tc.targetPath, target: &Target{url: "https://example.com/repo.git"}},
			}
			err := method.MethodEngine(context.Background(), conn, change, path)
			if (err != nil) != tc.fail || !reflect.DeepEqual(commands, tc.want) {
				t.Fatalf("commands=%v err=%v", commands, err)
			}
		})
	}
}
