// Package ota owns update eligibility and manifests, independently of HTTP and disk.
package ota

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
)

var ErrUnavailable = errors.New("firmware unavailable")

// Image must verify integrity before returning an independent, seekable reader.
type Image interface {
	Open() (io.ReadSeekCloser, error)
	CRC32() uint32
}

type Release struct {
	SourceVersion string
	Platform      string
	Version       string
	DownloadURL   string
	Critical      bool
	UpdateID      string
}

type Details struct {
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	FallbackURL string `json:"fallbackUrl"`
	CRC32       uint32 `json:"crc32"`
	Critical    bool   `json:"critical"`
	UpdateID    string `json:"updateId"`
	FallbackIP  string `json:"fallbackIp"`
}

type Manifest struct {
	HasUpdate bool `json:"hasUpdate"`
	// A nil Details leaves only hasUpdate:false in the disabled response.
	*Details
}

// Update is immutable. A nil *Update means OTA is disabled.
type Update struct {
	release Release
	image   Image
	url     url.URL
}

func NewUpdate(release Release, image Image) (*Update, error) {
	if image == nil {
		return nil, errors.New("firmware image is required")
	}
	if release.SourceVersion == "" || release.Platform == "" || release.Version == "" || release.UpdateID == "" {
		return nil, errors.New("source version, platform, target version and update ID are required")
	}
	u, err := url.Parse(release.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("firmware URL: %w", err)
	}
	ip, err := netip.ParseAddr(u.Hostname())
	portValid := true
	if u.Port() != "" {
		port, portErr := strconv.Atoi(u.Port())
		portValid = portErr == nil && port > 0 && port <= 65535
	} else if strings.HasSuffix(u.Host, ":") {
		portValid = false
	}
	if err != nil || !ip.Is4() || (!ip.IsPrivate() && !ip.IsLoopback()) ||
		u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		!portValid || u.RawPath != "" || u.EscapedPath() != u.Path || strings.ContainsAny(u.Path, "{}\\") ||
		!strings.HasPrefix(u.Path, "/ota/") || strings.HasSuffix(u.Path, "/") || path.Clean(u.Path) != u.Path {
		return nil, errors.New("firmware URL must be a local IPv4 HTTP URL under /ota/ without credentials, query or fragment")
	}
	return &Update{release: release, image: image, url: *u}, nil
}

func (u *Update) Check(version, platform string) (Manifest, error) {
	if u == nil || version != u.release.SourceVersion || (platform != "" && platform != u.release.Platform) {
		return Manifest{}, nil
	}
	image, err := u.Open()
	if err != nil {
		return Manifest{}, err
	}
	if err := image.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close verified firmware: %w", err)
	}
	return Manifest{HasUpdate: true, Details: &Details{
		Version:     u.release.Version,
		DownloadURL: u.release.DownloadURL,
		FallbackURL: u.release.DownloadURL,
		CRC32:       u.image.CRC32(),
		Critical:    u.release.Critical,
		UpdateID:    u.release.UpdateID,
		FallbackIP:  u.url.Hostname(),
	}}, nil
}

func (u *Update) Open() (io.ReadSeekCloser, error) {
	if u == nil {
		return nil, ErrUnavailable
	}
	return u.image.Open()
}

func (u *Update) DownloadPath() string {
	if u == nil {
		return ""
	}
	return u.url.Path
}
