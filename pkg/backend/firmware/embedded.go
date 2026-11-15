package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
)

// Embedded is for immutable embedded files, not mutable directory filesystems.
type Embedded struct {
	files fs.FS
	name  string
	crc   uint32
}

func NewEmbedded(files fs.FS, name, expectedSHA256 string) (*Embedded, error) {
	file, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, ok := file.(io.Seeker); !ok {
		return nil, errors.New("embedded firmware must support seeking")
	}
	digest, checksum := sha256.New(), crc32.NewIEEE()
	if _, err := io.Copy(io.MultiWriter(digest, checksum), file); err != nil {
		return nil, err
	}
	if hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 {
		return nil, fmt.Errorf("embedded firmware %q: SHA-256 mismatch", name)
	}
	return &Embedded{files: files, name: name, crc: checksum.Sum32()}, nil
}

func (e *Embedded) CRC32() uint32 { return e.crc }

func (e *Embedded) Open() (io.ReadSeekCloser, error) {
	file, err := e.files.Open(e.name)
	if err != nil {
		return nil, err
	}
	reader, ok := file.(io.ReadSeekCloser)
	if !ok {
		_ = file.Close()
		return nil, errors.New("embedded firmware must support seeking")
	}
	return reader, nil
}
