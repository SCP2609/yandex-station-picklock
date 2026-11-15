// Package firmware implements verified, streaming storage for OTA images.
package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// Keep the file unchanged while serving: verification precedes streaming.
type File struct {
	name   string
	digest [sha256.Size]byte
	size   int64
	crc32  uint32
}

// NewFile requires a SHA-256 obtained independently of the downloaded image.
func NewFile(name, expectedSHA256 string) (*File, error) {
	digest, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(digest) != sha256.Size {
		return nil, errors.New("expected firmware SHA-256 must contain 64 hexadecimal characters")
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return nil, fmt.Errorf("firmware path: %w", err)
	}
	image := &File{name: name, digest: [sha256.Size]byte(digest)}
	f, size, checksum, err := image.verifiedFile()
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close firmware: %w", err)
	}
	image.size, image.crc32 = size, checksum
	return image, nil
}

func (f *File) CRC32() uint32 { return f.crc32 }

// Open rechecks the image before every manifest or download; no shared offset.
func (f *File) Open() (io.ReadSeekCloser, error) {
	image, size, checksum, err := f.verifiedFile()
	if err != nil {
		return nil, err
	}
	if size != f.size || checksum != f.crc32 {
		_ = image.Close()
		return nil, errors.New("firmware metadata changed")
	}
	return image, nil
}

func (f *File) verifiedFile() (*os.File, int64, uint32, error) {
	info, err := os.Lstat(f.name)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("stat firmware: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, 0, 0, errors.New("firmware must be a nonempty regular file, not a symlink")
	}
	image, err := os.Open(f.name)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open firmware: %w", err)
	}
	valid := false
	defer func() {
		if !valid {
			_ = image.Close()
		}
	}()
	before, err := image.Stat()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("stat opened firmware: %w", err)
	}
	if !before.Mode().IsRegular() || !os.SameFile(info, before) {
		return nil, 0, 0, errors.New("firmware replaced while opening")
	}
	digest, checksum := sha256.New(), crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(digest, checksum), image)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("verify firmware: %w", err)
	}
	after, err := image.Stat()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("stat verified firmware: %w", err)
	}
	if n != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) ||
		[sha256.Size]byte(digest.Sum(nil)) != f.digest {
		return nil, 0, 0, errors.New("firmware changed or SHA-256 mismatch")
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		return nil, 0, 0, fmt.Errorf("rewind firmware: %w", err)
	}
	valid = true
	return image, n, checksum.Sum32(), nil
}
