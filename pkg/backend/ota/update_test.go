package ota_test

import (
	"bytes"
	"errors"
	"hash/crc32"
	"io"
	"testing"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

type image struct{}

func (image) CRC32() uint32 { return crc32.ChecksumIEEE([]byte("image")) }

type reader struct{ *bytes.Reader }

func (reader) Close() error { return nil }

func (image) Open() (io.ReadSeekCloser, error) {
	return reader{bytes.NewReader([]byte("image"))}, nil
}

func TestReleaseRejectsUnsafeOrAmbiguousURLs(t *testing.T) {
	for _, url := range []string{
		"http://8.8.8.8/ota/image.zip", "http://example.com/ota/image.zip",
		"https://127.0.0.1/ota/image.zip", "http://user@127.0.0.1/ota/image.zip",
		"http://127.0.0.1/ota/image.zip?q=1", "http://127.0.0.1/ota/image.zip?",
		"http://127.0.0.1/ota/image.zip#fragment", "http://127.0.0.1/other/image.zip",
		"http://127.0.0.1/ota/../image.zip", "http://127.0.0.1/ota/%2e%2e/image.zip",
		"http://127.0.0.1/ota/{wildcard}", "http://127.0.0.1/ota/",
		"http://127.0.0.1:0/ota/image.zip", "http://127.0.0.1:65536/ota/image.zip",
		"http://127.0.0.1:/ota/image.zip",
	} {
		t.Run(url, func(t *testing.T) {
			_, err := ota.NewUpdate(ota.Release{
				SourceVersion: "factory", Platform: "station", Version: "target", UpdateID: "id", DownloadURL: url,
			}, image{})
			if err == nil {
				t.Fatal("accepted invalid URL")
			}
		})
	}
}

func TestReleaseRequiresImageAndEligibility(t *testing.T) {
	release := ota.Release{
		SourceVersion: "factory", Platform: "station", Version: "target", UpdateID: "id",
		DownloadURL: "http://192.168.1.126/ota/image.zip",
	}
	if _, err := ota.NewUpdate(release, nil); err == nil {
		t.Fatal("accepted missing image")
	}
	release.Platform = ""
	if _, err := ota.NewUpdate(release, image{}); err == nil {
		t.Fatal("accepted missing platform")
	}
	var disabled *ota.Update
	if _, err := disabled.Open(); !errors.Is(err, ota.ErrUnavailable) {
		t.Fatalf("disabled open: %v", err)
	}
}
