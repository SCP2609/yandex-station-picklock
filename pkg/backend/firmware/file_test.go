package firmware_test

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/firmware"
)

func TestFileVerificationAndIndependentReaders(t *testing.T) {
	name := filepath.Join(t.TempDir(), "image.zip")
	data := []byte("verified firmware")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	image, err := firmware.NewFile(name, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	if image.CRC32() != crc32.ChecksumIEEE(data) {
		t.Fatal("incorrect manifest CRC32")
	}
	first, err := image.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	second, err := image.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := io.ReadAll(second)
	if err != nil || string(got) != string(data) {
		t.Fatalf("reader offsets are shared: %q, %v", got, err)
	}
	if _, err := firmware.NewFile(name, "bad"); err == nil {
		t.Fatal("accepted malformed SHA-256")
	}
	if _, err := firmware.NewFile(name, string(make([]byte, 64))); err == nil {
		t.Fatal("accepted invalid SHA-256")
	}
	other := sha256.Sum256([]byte("another image"))
	if _, err := firmware.NewFile(name, hex.EncodeToString(other[:])); err == nil {
		t.Fatal("accepted mismatched SHA-256")
	}
	link := filepath.Join(t.TempDir(), "symlink.zip")
	if err := os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	if _, err := firmware.NewFile(link, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("accepted symlink")
	}
}
