package engine

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func configDownloadPaths(t *testing.T) {
	t.Helper()
	oldPath, oldBackup, oldLogger := defaultConfigPath, defaultConfigBackup, logger
	directory := t.TempDir()
	defaultConfigPath = filepath.Join(directory, "config.yaml")
	defaultConfigBackup = filepath.Join(directory, "config-backup.yaml")
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { defaultConfigPath, defaultConfigBackup, logger = oldPath, oldBackup, oldLogger })
}

func TestConfigDownloadPreservesFilesOnInvalidResponse(t *testing.T) {
	configDownloadPaths(t)
	const original = "targetConfigs: []\n"
	const backup = "images: []\n"
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"server error", "500: Internal Server Error", 500},
		{"not found", "targetConfigs: []", 404},
		{"no content", "", 204},
		{"empty", "", 200},
		{"HTML", "<html>error</html>", 200},
		{"YAML syntax", "targetConfigs: [", 200},
		{"error mapping", "500: Internal Server Error", 200},
		{"wrong shape", "targetConfigs: invalid", 200},
		{"unknown field", "targetConfigz: []", 200},
		{"multiple documents", "targetConfigs: []\n---\nimages: []", 200},
		{"oversized", strings.Repeat("x", (1<<20)+1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(defaultConfigPath, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(defaultConfigBackup, []byte(backup), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			for _, initial := range []bool{false, true} {
				updated, err := downloadUpdateConfigFile(server.URL, true, initial, "", "", "")
				if updated || err == nil {
					t.Fatalf("invalid response accepted: updated=%v err=%v", updated, err)
				}
				for path, want := range map[string]string{defaultConfigPath: original, defaultConfigBackup: backup} {
					got, err := os.ReadFile(path)
					if err != nil || string(got) != want {
						t.Fatalf("%s changed: %q %v", path, got, err)
					}
				}
			}
		})
	}
}

func TestConfigDownloadUpdateAndBackup(t *testing.T) {
	configDownloadPaths(t)
	original := "targetConfigs: []\n"
	desired := "configReload:\n  configURL: https://example.com/config.yaml\n  schedule: '*/1 * * * *'\ntargetConfigs: []\n"
	if err := os.WriteFile(defaultConfigPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token temporary-token" {
			t.Error("missing token authentication")
		}
		_, _ = w.Write([]byte(desired))
	}))
	defer server.Close()
	updated, err := downloadUpdateConfigFile(server.URL, true, false, "temporary-token", "", "")
	if err != nil || !updated {
		t.Fatalf("update failed: %v", err)
	}
	for path, want := range map[string]string{defaultConfigPath: desired, defaultConfigBackup: original} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", path, got, err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("config permissions changed")
		}
	}
	updated, err = downloadUpdateConfigFile(server.URL, true, false, "temporary-token", "", "")
	if err != nil || updated {
		t.Fatalf("unchanged response: %v %v", updated, err)
	}
	entries, _ := filepath.Glob(filepath.Join(filepath.Dir(defaultConfigPath), ".fetchit-config-*"))
	if len(entries) != 0 {
		t.Fatal("staged files leaked")
	}
}

func TestConfigDownloadInitialAndWriteFailure(t *testing.T) {
	configDownloadPaths(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("targetConfigs: []\n")) }))
	defer server.Close()
	updated, err := downloadUpdateConfigFile(server.URL, false, true, "", "", "")
	if err != nil || !updated {
		t.Fatalf("initial download: %v %v", updated, err)
	}
	if _, err := os.Stat(defaultConfigBackup); !os.IsNotExist(err) {
		t.Fatal("initial download created backup")
	}
	if err := os.Remove(defaultConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(defaultConfigPath, 0700); err != nil {
		t.Fatal(err)
	}
	updated, err = downloadUpdateConfigFile(server.URL, false, true, "", "", "")
	if updated || err == nil {
		t.Fatal("rename failure accepted")
	}
	info, err := os.Stat(defaultConfigPath)
	if err != nil || !info.IsDir() {
		t.Fatal("destination replaced on failure")
	}
}
