package backend_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/firmware"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/quasar"
)

const imagePath = "/ota/test-image.zip"

func updateFixture(t *testing.T) (*ota.Update, string, []byte) {
	t.Helper()
	data := []byte("synthetic firmware for transport tests")
	name := filepath.Join(t.TempDir(), "image.zip")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	image, err := firmware.NewFile(name, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	update, err := ota.NewUpdate(ota.Release{
		SourceVersion: "factory", Platform: "yandexstation", Version: "target",
		DownloadURL: "http://127.0.0.1:8080" + imagePath,
		Critical:    true, UpdateID: "82789",
	}, image)
	if err != nil {
		t.Fatal(err)
	}
	return update, name, data
}

func request(handler http.Handler, method, host, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestDomainsAndOfflineRoutes(t *testing.T) {
	handler := backend.NewRouter(nil)
	for _, tc := range []struct {
		method, host, path string
		status             int
	}{
		{"POST", "oauth.yandex.ru", "/token", 200},
		{"POST", "OAUTH.YANDEX.RU.:443", "/token", 200},
		{"GET", "oauth.yandex.ru", "/token", 502},
		{"POST", "quasar.yandex.net", "/token", 502},
		{"POST", "evil.example", "/token", 502},
		{"POST", "quasar.yandex.net", "/register", 200},
		{"GET", "quasar.yandex.ru", "/get_sync_info", 200},
		{"GET", "scbh.yandex.net", "/check_updates?version=factory", 200},
		{"GET", "quasar.yandex.net", "/unknown", 502},
		{"GET", "quasar.yandex.net", "/not_check_updates", 502},
		{"GET", "evil.example", "/check_updates?version=factory", 502},
		{"GET", "localhost", "/health", 200},
		{"HEAD", "localhost", "/health", 200},
		{"GET", "quasar.yandex.net", "/generate_204", 204},
		{"GET", "localhost", "/dev/static/speed/200kb", 200},
		{"GET", "quasar.yandex.net", imagePath, 404},
	} {
		t.Run(tc.method+" "+tc.host+tc.path, func(t *testing.T) {
			w := request(handler, tc.method, tc.host, tc.path)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
			if (tc.method == "HEAD" || tc.status == 204) && w.Body.Len() != 0 {
				t.Fatal("unexpected response body")
			}
			if strings.Contains(tc.path, "200kb") && w.Body.Len() != 200*1024 {
				t.Fatalf("speed payload length = %d", w.Body.Len())
			}
		})
	}
	w := request(handler, "POST", "oauth.yandex.ru", "/token")
	var token map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	if token["access_token"] != "local-offline-access" || token["expires_in"] != float64(31536000) {
		t.Fatalf("unexpected token response: %v", token)
	}
	w = request(handler, "POST", "quasar.yandex.net", "/register")
	if w.Body.String() != `{"message":"allvalid","status":"ok"}` {
		t.Fatalf("unexpected registration: %s", w.Body.String())
	}
}

func TestOTAEligibilityAndManifest(t *testing.T) {
	update, _, data := updateFixture(t)
	handler := backend.NewRouter(update)
	for _, host := range []string{"quasar.yandex.net", "quasar.yandex.ru", "scbh.yandex.net"} {
		for _, tc := range []struct {
			query string
			want  bool
		}{
			{"version=factory", true},
			{"version=factory&platform=yandexstation", true},
			{"version=factory&platform=", true},
			{"version=target", false},
			{"version=factory&platform=other", false},
			{"version=factory&version=other", false},
			{"version=factory&platform=&platform=other", false},
			{"version=factory&bad=%ZZ", false},
			{"", false},
		} {
			w := request(handler, "POST", host, "/check_updates?"+tc.query)
			var manifest ota.Manifest
			if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || manifest.HasUpdate != tc.want {
				t.Fatalf("%s %s: status=%d, manifest=%s", host, tc.query, w.Code, w.Body.String())
			}
			if tc.want {
				if manifest.Details == nil || manifest.CRC32 != crc32.ChecksumIEEE(data) || manifest.Version != "target" ||
					manifest.DownloadURL != manifest.FallbackURL || manifest.FallbackIP != "127.0.0.1" ||
					!manifest.Critical || manifest.UpdateID != "82789" {
					t.Fatalf("unexpected release metadata: %s", w.Body.String())
				}
			} else if w.Body.String() != `{"hasUpdate":false}` {
				t.Fatalf("disabled manifest leaked metadata: %s", w.Body.String())
			}
		}
	}
	for _, path := range []string{"/allow_update?version=factory", "/v2/check_updates?version=target"} {
		if body := request(handler, "GET", "quasar.yandex.net", path).Body.String(); body != `{"hasUpdate":false}` {
			t.Fatalf("%s: %s", path, body)
		}
	}
}

func TestOTADownloadAndRanges(t *testing.T) {
	update, _, data := updateFixture(t)
	handler := backend.NewRouter(update)
	for _, tc := range []struct {
		method, byteRange string
		status            int
		body              string
	}{
		{"GET", "", 200, string(data)},
		{"HEAD", "", 200, ""},
		{"GET", "bytes=2-5", 206, string(data[2:6])},
		{"GET", "bytes=-4", 206, string(data[len(data)-4:])},
		{"GET", "bytes=10-", 206, string(data[10:])},
		{"GET", "bytes=999-", 416, ""},
		{"POST", "", 405, ""},
	} {
		r := httptest.NewRequest(tc.method, imagePath, nil)
		r.Host = "127.0.0.1:8080"
		if tc.byteRange != "" {
			r.Header.Set("Range", tc.byteRange)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || (tc.status < 400 && w.Body.String() != tc.body) {
			t.Fatalf("%s %s: status=%d, body=%q", tc.method, tc.byteRange, w.Code, w.Body.String())
		}
		if tc.status == 206 && w.Header().Get("Content-Range") == "" {
			t.Fatal("missing Content-Range")
		}
		if tc.method == "HEAD" && w.Header().Get("Content-Length") != strconv.Itoa(len(data)) {
			t.Fatalf("HEAD Content-Length = %q", w.Header().Get("Content-Length"))
		}
	}
}

func TestMissingOrCorruptImageIsNeverAdvertised(t *testing.T) {
	for _, mutation := range []string{"corrupt", "remove"} {
		t.Run(mutation, func(t *testing.T) {
			update, name, data := updateFixture(t)
			handler := backend.NewRouter(update)
			var err error
			if mutation == "corrupt" {
				err = os.WriteFile(name, []byte(strings.Repeat("X", len(data))), 0600)
			} else {
				err = os.Remove(name)
			}
			if err != nil {
				t.Fatal(err)
			}
			if body := request(handler, "GET", "quasar.yandex.net", "/check_updates?version=factory").Body.String(); body != `{"hasUpdate":false}` {
				t.Fatalf("advertised unavailable image: %s", body)
			}
			if w := request(handler, "GET", "quasar.yandex.net", imagePath); w.Code != 404 {
				t.Fatalf("download status = %d", w.Code)
			}
		})
	}
}

func TestSyncPlatformConfiguration(t *testing.T) {
	handler := backend.NewRouter(nil)
	for _, platform := range []string{"", "yandexstation", "yandexstation_2", "yandexmini"} {
		w := request(handler, "GET", "quasar.yandex.net", "/get_sync_info?platform="+platform)
		var info quasar.SyncInfo
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		wantNetwork := platform == "yandexmini"
		for _, adbd := range []quasar.ADBConfig{info.ADBD, info.Config.ADBD, info.Config.Device.ADBD, info.Config.System.ADBD} {
			if !adbd.RemoteActivation || adbd.Network != wantNetwork || adbd.FullDurationMS != 31536000000 {
				t.Fatalf("%s: unexpected ADB config: %+v", platform, adbd)
			}
		}
		if (info.Config.System.TVServices != nil) != (platform == "yandexstation_2") {
			t.Fatalf("%s: unexpected TV services: %+v", platform, info.Config.System.TVServices)
		}
	}
}

func TestRequestBodyLimits(t *testing.T) {
	handler := backend.NewRouter(nil)
	for _, tc := range []struct {
		name   string
		length int64
		body   io.Reader
		status int
	}{
		{"declared-too-large", (8 << 20) + 1, strings.NewReader(""), 413},
		{"chunked-too-large", -1, strings.NewReader(strings.Repeat("x", (8<<20)+1)), 413},
		{"exact-limit", 8 << 20, strings.NewReader(strings.Repeat("x", 8<<20)), 200},
		{"read-error", -1, iotest.ErrReader(io.ErrUnexpectedEOF), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/register", nil)
			r.Host = "quasar.yandex.net"
			r.ContentLength = tc.length
			r.Body = io.NopCloser(tc.body)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
		})
	}
}
