// Package boot prepares the single Station 1 layout verified in checkpoint2_boot.
package boot

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	Size                  = 16 * 1024 * 1024
	StockSHA256           = "d83a3258aae6cf5a696dc12b048156950d6313e73a6d3712350a361c755e29df"
	RootSHA256            = "75cd84ee500e42f01230b2e7b8dd949e3dc7638befe582d4266901bcb5b1550d"
	NoTelnetSHA256        = "2a50457502f55b49038787c3319a180def35e4ec5b3b3e923deb1ec03164ba94"
	tailOffset            = 14577664
	noTelnetRamdiskSHA256 = "add1c57d83e47f906dad65dbfd98e763566c61138c9b64a60b34be93bc38ab49"
)

const telnetService = "service rootshell /system/bin/busybox-smp telnetd -l /system/bin/sh -p 2323 -F\n    class core\n\n"

func SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func Verify(data []byte, expected string) error {
	if len(data) != Size || SHA256(data) != expected {
		return errors.New("unexpected Station 1 boot size or SHA-256")
	}
	_, err := parseImage(data)
	return err
}

// Prepare preserves the device's kernel, header, CPIO metadata and fixed tail.
// Reuse the verified compressed ramdisk: Go's deflater exceeds this tiny slot.
func Prepare(stock, reference []byte) ([]byte, error) {
	if err := Verify(stock, StockSHA256); err != nil {
		return nil, fmt.Errorf("stock boot: %w", err)
	}
	if err := Verify(reference, RootSHA256); err != nil {
		return nil, fmt.Errorf("reference boot: %w", err)
	}
	original, _ := parseImage(stock)
	candidate, _ := parseImage(reference)
	changes := map[string]bool{"default.prop": true, "init": true, "init.rc": true, "sbin/adbd": true, "sbin/healthd": true, "selinux_version": true, "sepolicy": true}
	if err := compareArchives(original.archive, candidate.archive, changes); err != nil {
		return nil, err
	}
	return rebuild(stock, candidate.ramdisk, RootSHA256)
}

// DisableTelnet changes only init.rc; SSH must already have been verified by the caller.
func DisableTelnet(current, packed []byte) ([]byte, error) {
	if err := Verify(current, RootSHA256); err != nil {
		return nil, err
	}
	if SHA256(packed) != noTelnetRamdiskSHA256 {
		return nil, errors.New("unexpected SSH-only ramdisk SHA-256")
	}
	original, _ := parseImage(current)
	replacement, err := readArchive(packed)
	if err != nil {
		return nil, err
	}
	if err := compareArchives(original.archive, replacement, map[string]bool{"init.rc": true}); err != nil {
		return nil, err
	}
	for i, entry := range original.archive.entries {
		if entry.name == "init.rc" {
			if bytes.Count(entry.body, []byte(telnetService)) != 1 || !bytes.Equal(bytes.Replace(entry.body, []byte(telnetService), nil, 1), replacement.entries[i].body) {
				return nil, errors.New("SSH-only init.rc changes more than the Telnet service")
			}
		}
	}
	return rebuild(current, packed, NoTelnetSHA256)
}

type image struct {
	page    int
	kernel  []byte
	ramdisk []byte
	archive archive
}

func parseImage(data []byte) (image, error) {
	if len(data) != Size || string(data[:8]) != "ANDROID!" {
		return image{}, errors.New("invalid Android boot header")
	}
	page := int(binary.LittleEndian.Uint32(data[36:]))
	if page != 2048 || binary.LittleEndian.Uint32(data[40:]) != 0 || binary.LittleEndian.Uint32(data[24:]) != 0 {
		return image{}, errors.New("unsupported boot page, header version or second stage")
	}
	ks, rs := int(binary.LittleEndian.Uint32(data[8:])), int(binary.LittleEndian.Uint32(data[16:]))
	start := align(page+ks, page)
	if ks <= 0 || rs <= 0 || start > tailOffset || rs > tailOffset-start {
		return image{}, errors.New("boot sections exceed the preserved tail")
	}
	result := image{page: page, kernel: data[page : page+ks], ramdisk: data[start : start+rs]}
	if !bytes.Equal(data[576:608], imageID(result.kernel, result.ramdisk)) {
		return image{}, errors.New("Android boot ID mismatch")
	}
	parsed, err := readArchive(result.ramdisk)
	result.archive = parsed
	return result, err
}

func rebuild(original, packed []byte, expected string) ([]byte, error) {
	source, err := parseImage(original)
	if err != nil {
		return nil, err
	}
	start := align(source.page+len(source.kernel), source.page)
	if len(packed) > tailOffset-start {
		return nil, errors.New("modified ramdisk would overwrite the stock tail")
	}
	result := bytes.Clone(original)
	clear(result[start:tailOffset])
	copy(result[start:], packed)
	binary.LittleEndian.PutUint32(result[16:], uint32(len(packed)))
	copy(result[576:608], imageID(source.kernel, packed))
	if !bytes.Equal(result[tailOffset:], original[tailOffset:]) {
		return nil, errors.New("boot tail changed")
	}
	if err := Verify(result, expected); err != nil {
		return nil, fmt.Errorf("rebuilt boot: %w", err)
	}
	return result, nil
}

func imageID(kernel, ramdisk []byte) []byte {
	// Legacy Android ID is SHA-1 over each section followed by its uint32 size.
	digest := sha1.New()
	for _, section := range [][]byte{kernel, ramdisk, nil} {
		_, _ = digest.Write(section)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(section)))
		_, _ = digest.Write(size[:])
	}
	return append(digest.Sum(nil), make([]byte, 12)...)
}

type entry struct {
	name string
	// newc order: inode, mode, uid, gid, nlink, mtime, size, device major/minor,
	// rdev major/minor, name size (including NUL), optional additive checksum.
	fields [13]uint32
	body   []byte
}
type archive struct {
	entries []entry
	trailer []byte
}

func readArchive(packed []byte) (archive, error) {
	reader := bytes.NewReader(packed)
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return archive{}, err
	}
	gz.Multistream(false)
	const limit = 64 << 20
	raw, readErr := io.ReadAll(io.LimitReader(gz, limit+1))
	closeErr := gz.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return archive{}, err
	}
	if len(raw) > limit || reader.Len() != 0 {
		return archive{}, errors.New("ramdisk is too large or has trailing gzip members")
	}
	var result archive
	seen := make(map[string]bool)
	for pos := 0; ; {
		if pos > len(raw)-110 {
			return archive{}, errors.New("truncated CPIO header")
		}
		header := raw[pos : pos+110]
		if string(header[:6]) != "070701" && string(header[:6]) != "070702" {
			return archive{}, errors.New("unsupported CPIO format")
		}
		var fields [13]uint32
		for i := range fields {
			value, err := strconv.ParseUint(string(header[6+i*8:14+i*8]), 16, 32)
			if err != nil {
				return archive{}, fmt.Errorf("CPIO field: %w", err)
			}
			fields[i] = uint32(value)
		}
		nameSize, bodySize := int(fields[11]), int(fields[6])
		nameStart := pos + 110
		if nameSize < 2 || nameSize > len(raw)-nameStart {
			return archive{}, errors.New("invalid CPIO name size")
		}
		nameBytes := raw[nameStart : nameStart+nameSize]
		name := string(nameBytes[:len(nameBytes)-1])
		if nameBytes[len(nameBytes)-1] != 0 || strings.ContainsRune(name, 0) || !utf8.ValidString(name) || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return archive{}, errors.New("unsafe CPIO name")
		}
		start := align(nameStart+nameSize, 4)
		if start > len(raw) || bodySize > len(raw)-start {
			return archive{}, errors.New("truncated CPIO payload")
		}
		end := start + bodySize
		next := align(end, 4)
		if next > len(raw) || !zero(raw[nameStart+nameSize:start]) || !zero(raw[end:next]) {
			return archive{}, errors.New("invalid CPIO padding")
		}
		body := raw[start:end]
		var checksum uint32
		if string(header[:6]) == "070702" {
			for _, b := range body {
				checksum += uint32(b)
			}
		}
		if fields[12] != checksum {
			return archive{}, errors.New("CPIO checksum mismatch")
		}
		if name == "TRAILER!!!" {
			if bodySize != 0 || !zero(raw[next:]) || len(result.entries) != 41 {
				return archive{}, errors.New("unexpected CPIO trailer or entry count")
			}
			result.trailer = raw[pos:]
			return result, nil
		}
		if seen[name] || fields[4] != 1 {
			return archive{}, errors.New("CPIO duplicate or hardlink")
		}
		seen[name] = true
		result.entries = append(result.entries, entry{name: name, fields: fields, body: body})
		pos = next
	}
}

func compareArchives(original, candidate archive, changes map[string]bool) error {
	if len(original.entries) != len(candidate.entries) || !bytes.Equal(original.trailer, candidate.trailer) {
		return errors.New("CPIO structure or trailer changed")
	}
	actual := make(map[string]bool)
	for i, old := range original.entries {
		new := candidate.entries[i]
		if old.name != new.name {
			return errors.New("CPIO order changed")
		}
		for field := range old.fields {
			// Only payload length and the optional CPIO CRC may change.
			if field != 6 && field != 12 && old.fields[field] != new.fields[field] {
				return fmt.Errorf("CPIO metadata changed: %s", old.name)
			}
		}
		if !bytes.Equal(old.body, new.body) {
			actual[old.name] = true
		}
	}
	if len(actual) != len(changes) {
		return errors.New("unexpected modified ramdisk files")
	}
	for name := range changes {
		if !actual[name] {
			return fmt.Errorf("missing expected ramdisk change: %s", name)
		}
	}
	return nil
}

func align(n, boundary int) int { return (n + boundary - 1) / boundary * boundary }
func zero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}
