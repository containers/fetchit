package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type downloadTransport func(*http.Request) (*http.Response, error)

func (f downloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type downloadBody struct {
	io.Reader
	closed bool
}

func (b *downloadBody) Close() error { b.closed = true; return nil }

func mockDownload(t *testing.T, status int, reader io.Reader) *downloadBody {
	t.Helper()
	body := &downloadBody{Reader: reader}
	old := http.DefaultTransport
	http.DefaultTransport = downloadTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: body, Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	return body
}

func TestHTTPDownloadsRejectStatusAndCloseBody(t *testing.T) {
	for _, download := range []struct {
		name string
		run  func(string) error
	}{
		{"image", func(url string) error {
			return (&Image{}).loadHTTPPodmanAtPath(context.Background(), context.Background(), url, "archive.zip")
		}},
		{"archive", extractZip},
	} {
		t.Run(download.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, status := range []int{http.StatusNoContent, http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
				t.Run(strconv.Itoa(status), func(t *testing.T) {
					body := mockDownload(t, status, strings.NewReader("error page"))
					err := download.run("https://example.invalid/archive.zip")
					if err == nil || !strings.Contains(err.Error(), strconv.Itoa(status)) {
						t.Fatalf("expected HTTP %d error, got %v", status, err)
					}
					if !body.closed {
						t.Fatal("response body was not closed")
					}
					entries, err := os.ReadDir(".")
					if err != nil || len(entries) != 0 {
						t.Fatalf("failed response created files: %v, %v", entries, err)
					}
				})
			}
		})
	}
}

func TestImageDownloadClosesBodyWhenAlreadyPresent(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	body := mockDownload(t, http.StatusOK, strings.NewReader("unused"))
	err := (&Image{}).loadHTTPPodmanAtPath(context.Background(), context.Background(), "https://example.invalid/image.tar", destination)
	if err != nil {
		t.Fatal(err)
	}
	if !body.closed {
		t.Fatal("response body was not closed on skipped download")
	}
}

type failedDownloadReader struct{ err error }

func (r failedDownloadReader) Read([]byte) (int, error) { return 0, r.err }

func TestArchiveDownloadClosesBodyOnProcessingFailure(t *testing.T) {
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	for _, tc := range []struct {
		name   string
		reader io.Reader
		want   error
	}{
		{"invalid ZIP", strings.NewReader("not a ZIP"), nil},
		{"body read failure", io.MultiReader(strings.NewReader("partial ZIP bytes"), failedDownloadReader{io.ErrUnexpectedEOF}), io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			body := mockDownload(t, http.StatusOK, tc.reader)
			err := extractZip("https://example.invalid/fetchit-http-download-test.zip")
			if err == nil {
				t.Fatal("expected processing error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("lost read error: %v", err)
			}
			if !body.closed {
				t.Fatal("response body was not closed on processing failure")
			}
			entries, readErr := os.ReadDir(".")
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("processing failure left artifacts: %v %v", entries, readErr)
			}
		})
	}
}

func TestImageHTTPFailurePreservesExistingDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(destination, []byte("existing image"), 0600); err != nil {
		t.Fatal(err)
	}
	body := mockDownload(t, http.StatusInternalServerError, strings.NewReader("error page"))
	err := (&Image{}).loadHTTPPodmanAtPath(context.Background(), context.Background(), "https://example.invalid/image.tar", destination)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != 500 {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "existing image" {
		t.Fatalf("destination changed: %q %v", data, err)
	}
	if !body.closed {
		t.Fatal("body not closed")
	}
}

func TestDisconnectedCallersPropagateHTTPFailure(t *testing.T) {
	for _, name := range []string{"clone", "update", "quadlet"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			body := mockDownload(t, http.StatusUnauthorized, strings.NewReader("unauthorized"))
			target := &Target{url: "https://example.invalid/repo.zip", disconnected: true}
			method := &Quadlet{CommonMethod: CommonMethod{target: target}}
			var err error
			switch name {
			case "clone":
				err = getDisconnected(target)
			case "update":
				err = currentToLatest(context.Background(), context.Background(), method, target, nil)
			case "quadlet":
				err = method.reconcile(context.Background(), context.Background())
			}
			var status *HTTPStatusError
			if !errors.As(err, &status) || status.StatusCode != 401 {
				t.Fatalf("archive error lost: %v", err)
			}
			if !body.closed {
				t.Fatal("body not closed")
			}
		})
	}
}

func zipEntries(t *testing.T, names ...string) []*zip.File {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "/") {
			continue
		}
		if _, err := entry.Write([]byte("payload")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return reader.File
}

func TestArchiveRejectsEscapingPathsBeforeExtraction(t *testing.T) {
	for _, name := range []string{"../archive-evil/file", "../../outside", "/absolute"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			directory := filepath.Join(parent, "archive")
			if err := extractArchive(zipEntries(t, "valid/file", name), directory); err == nil {
				t.Fatal("accepted escaping ZIP path")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("unsafe archive wrote files: %v %v", entries, err)
			}
		})
	}
}

func TestArchiveExtractsValidEntries(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "archive")
	if err := extractArchive(zipEntries(t, "nested/file", ".git/HEAD"), directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nested/file", ".git/HEAD"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(data) != "payload" {
			t.Fatalf("bad extracted file: %q %v", data, err)
		}
	}
}

type closeErrorWriter struct {
	bytes.Buffer
	err    error
	closed bool
}

func (w *closeErrorWriter) Close() error { w.closed = true; return w.err }

func TestDownloadCopyReportsCloseAndReadErrors(t *testing.T) {
	closeErr := errors.New("delayed write failure")
	for _, reader := range []io.Reader{strings.NewReader("complete"), io.MultiReader(strings.NewReader("partial"), failedDownloadReader{io.ErrUnexpectedEOF})} {
		destination := &closeErrorWriter{err: closeErr}
		err := copyAndClose(destination, reader)
		if !destination.closed || !errors.Is(err, closeErr) {
			t.Fatalf("lost close error: %v", err)
		}
	}
}

func TestImageIncompleteDownloadRemovesTemporaryFile(t *testing.T) {
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	directory := t.TempDir()
	body := mockDownload(t, http.StatusOK, io.MultiReader(strings.NewReader("partial image"), failedDownloadReader{io.ErrUnexpectedEOF}))
	err := (&Image{}).loadHTTPPodmanAtPath(context.Background(), context.Background(), "https://example.invalid/image.tar", filepath.Join(directory, "image.tar"))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("lost download error: %v", err)
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("incomplete image left files: %v %v", entries, readErr)
	}
	if !body.closed {
		t.Fatal("body not closed")
	}
}

func TestArchiveExistingSymlinkCannotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "archive")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(zipEntries(t, "link/file"), directory); err == nil {
		t.Fatal("accepted escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatalf("wrote outside root: %v", err)
	}
}

func TestImageHTTPFailureIsLoggedAtErrorLevel(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	oldLogger := logger
	logger = zap.New(core).Sugar()
	t.Cleanup(func() { logger = oldLogger })
	body := mockDownload(t, http.StatusUnauthorized, strings.NewReader("unauthorized"))
	image := &Image{Url: "https://example.invalid/image.tar", CommonMethod: CommonMethod{target: &Target{url: "repository"}}}
	image.Process(context.Background(), context.Background(), 0)
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 1 || !strings.Contains(logs.All()[0].Message, "HTTP 401") {
		t.Fatalf("download error was not visible: %v", logs.All())
	}
	if !body.closed {
		t.Fatal("body not closed")
	}
}

func TestArchiveExtractsLegacyParentPrefixedEntries(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "archive")
	// The disconnected CI fixture runs zip from inside the repository, giving
	// entries a ../archive/ prefix that resolves back inside the destination.
	if err := extractArchive(zipEntries(t, "../archive/", "../archive/.git/HEAD", "../archive/nested/file"), directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".git/HEAD", "nested/file"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(data) != "payload" {
			t.Fatalf("bad extracted file: %q %v", data, err)
		}
	}
}
