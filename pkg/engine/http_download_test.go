package engine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"
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
			return (&Image{}).loadHTTPPodman(context.Background(), context.Background(), url)
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
	// The existing parent directory ensures the image is skipped without Podman.
	body := mockDownload(t, http.StatusOK, strings.NewReader("unused"))
	err := (&Image{}).loadHTTPPodman(context.Background(), context.Background(), "https://example.invalid/..")
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
		{"body read failure", failedDownloadReader{io.ErrUnexpectedEOF}, io.ErrUnexpectedEOF},
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
		})
	}
}
