package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.2.0", -1},
		{"2.0.0", "1.9.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"v1.1.0", "1.2.4", -1},
		{"1.0", "1.0.0", 0},
		{"1", "1.0.0", 0},
		{"1.0.0-beta", "1.0.0", -1},
		{"1.0.0", "1.0.0-beta", 1},
		{"1.0.0-alpha.2", "1.0.0-alpha.10", -1},
		{"1.0.0-rc.1", "1.0.0-beta.2", 1},
		{"1.0.0+build", "1.0.0", 0},
		{"1.0.0+build.1", "1.0.0+build.2", 0},
		{"1.0.0-alpha+build", "1.0.0-alpha", 0},
		{"1.0.0-alpha.2+build.7", "1.0.0-alpha.10", -1},
		{"vv1.2.3", "1.2.3", 0},
		{"10.0.0", "9.99.99", 1},
	}
	for _, c := range cases {
		got, err := compareSemver(c.a, c.b)
		if err != nil {
			t.Fatalf("compareSemver(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseSemverInvalid(t *testing.T) {
	for _, v := range []string{"", "abc", "1.x.0", "1.2.3.4", "-1", "1.2.3-", "1.2.3+"} {
		if _, err := parseSemver(v); err == nil {
			t.Errorf("parseSemver(%q): expected error, got nil", v)
		}
	}
}

func TestLookupChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SHA256SUMS")
	content := "abc123 *helium-drm-fixer-linux-amd64\r\n" +
		"ffd2a4b4a4a4  helium-drm-fixer-windows-amd64.exe\r\n" +
		"\r\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if got, ok := lookupChecksum(path, "helium-drm-fixer-windows-amd64.exe"); !ok || got != "ffd2a4b4a4a4" {
		t.Errorf("lookupChecksum(windows exe) = %q, %v; want %q, true", got, ok, "ffd2a4b4a4a4")
	}
	if got, ok := lookupChecksum(path, "helium-drm-fixer-linux-amd64"); !ok || got != "abc123" {
		t.Errorf("lookupChecksum(linux) = %q, %v; want %q, true", got, ok, "abc123")
	}
	if _, ok := lookupChecksum(path, "helium-drm-fixer-darwin-arm64"); ok {
		t.Error("lookupChecksum(missing asset): expected false")
	}
}

func TestAssetName(t *testing.T) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	want := fmt.Sprintf("helium-drm-fixer-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
	if got := assetName(); got != want {
		t.Errorf("assetName() = %q, want %q", got, want)
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v1.2.3":  "1.2.3",
		"V1.2.3":  "1.2.3",
		"vv1.2.3": "1.2.3",
		" 1.2.3 ": "1.2.3",
		"vV1.2.3": "1.2.3",
		"1.2.3":   "1.2.3",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetCacheDirOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	got, err := getCacheDir(dir)
	if err != nil {
		t.Fatalf("getCacheDir: %v", err)
	}
	if got != dir {
		t.Fatalf("getCacheDir = %q, want %q", got, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("cache override was not created: %v", err)
	}
}

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "helium-drm-fixer")
	asset := filepath.Join(dir, "download")

	writeTestFile(t, exe, "old")
	writeTestFile(t, asset, "new")

	backup, err := replaceBinary(asset, exe)
	if err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	if backup != exe+".old" {
		t.Fatalf("backup = %q, want %q", backup, exe+".old")
	}
	if got := readTestFile(t, exe); got != "new" {
		t.Fatalf("installed binary = %q, want %q", got, "new")
	}
	if got := readTestFile(t, backup); got != "old" {
		t.Fatalf("backup = %q, want %q", got, "old")
	}

	// A second update replaces the previous backup.
	writeTestFile(t, asset, "newer")
	backup, err = replaceBinary(asset, exe)
	if err != nil {
		t.Fatalf("second replaceBinary: %v", err)
	}
	if got := readTestFile(t, exe); got != "newer" {
		t.Fatalf("installed binary = %q, want %q", got, "newer")
	}
	if got := readTestFile(t, backup); got != "new" {
		t.Fatalf("backup = %q, want %q", got, "new")
	}
}

func TestReplaceBinaryRollback(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "helium-drm-fixer")
	asset := filepath.Join(dir, "download")
	writeTestFile(t, exe, "old")
	writeTestFile(t, asset, "new")

	calls := 0
	rename := func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("simulated rename failure")
		}
		return os.Rename(oldPath, newPath)
	}

	if _, err := replaceBinaryWith(asset, exe, rename); err == nil {
		t.Fatal("expected the final rename to fail")
	}
	if got := readTestFile(t, exe); got != "old" {
		t.Fatalf("rollback failed: executable = %q, want %q", got, "old")
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatalf("staged file was not cleaned up after rollback: %v", err)
	}
	if calls != 3 {
		t.Fatalf("rename calls = %d, want 3", calls)
	}
}

func TestReplaceBinaryMissingAsset(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "helium-drm-fixer")
	writeTestFile(t, exe, "old")

	if _, err := replaceBinary(filepath.Join(dir, "missing"), exe); err == nil {
		t.Fatal("expected an error for a missing asset")
	}
	if got := readTestFile(t, exe); got != "old" {
		t.Fatalf("executable changed after a failed update: %q", got)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatalf("staged file was not cleaned up: %v", err)
	}
}

func TestReplaceBinaryStaleBackup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "helium-drm-fixer")
	writeTestFile(t, exe, "old")

	// A non-empty directory at the backup path cannot be removed, emulating a
	// locked/leftover ".old" file. The update must still succeed.
	if err := os.MkdirAll(filepath.Join(exe+".old", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(exe+".old", "nested", "keep"), "x")

	asset := filepath.Join(dir, "download")
	writeTestFile(t, asset, "new")

	backup, err := replaceBinary(asset, exe)
	if err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	if backup == exe+".old" {
		t.Fatalf("expected a unique backup path, got %q", backup)
	}
	if got := readTestFile(t, exe); got != "new" {
		t.Fatalf("installed binary = %q, want %q", got, "new")
	}
}

func TestVerifyChecksum(t *testing.T) {
	assetData := []byte("binary")
	sum := sha256.Sum256(assetData)
	hash := hex.EncodeToString(sum[:])

	dir := t.TempDir()
	assetPath := filepath.Join(dir, "asset")
	if err := os.WriteFile(assetPath, assetData, 0o755); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	asset := &ghAsset{Name: "asset"}

	verify := func(sumsBody string) error {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(sumsBody))
		}))
		defer server.Close()

		release := &ghRelease{
			TagName: "v9.9.9",
			Assets:  []ghAsset{{Name: "SHA256SUMS", BrowserDownloadURL: server.URL}},
		}
		return verifyChecksum(release, asset, assetPath, cacheDir)
	}

	if err := verify(hash + "  asset\n"); err != nil {
		t.Fatalf("valid checksum: %v", err)
	}
	if err := verify(hash + "  other\n"); err == nil {
		t.Fatal("missing checksum entry: expected an error")
	}
	if err := verify(strings.Repeat("0", 64) + "  asset\n"); err == nil {
		t.Fatal("checksum mismatch: expected an error")
	}

	release := &ghRelease{TagName: "v9.9.9"}
	if err := verifyChecksum(release, asset, assetPath, cacheDir); err == nil {
		t.Fatal("missing SHA256SUMS: expected an error")
	}
}

func TestRunCheckOnly(t *testing.T) {
	withReleaseServer(t, "v2.0.0", []ghAsset{{Name: assetName(), Size: 1234}})

	called := false
	res, err := Run(Options{
		Current:   "1.0.0",
		CheckOnly: true,
		BeforeInstall: func(*Result) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Fatal("BeforeInstall must not run in --check mode")
	}
	if !res.UpdateAvailable || res.To != "2.0.0" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Updated {
		t.Fatal("CheckOnly must not update the binary")
	}

	res, err = Run(Options{Current: "2.0.0", CheckOnly: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.UpdateAvailable {
		t.Fatalf("unexpected update for the current version: %+v", res)
	}
}

func TestRunForceWithoutVersion(t *testing.T) {
	withReleaseServer(t, "v2.0.0", []ghAsset{{Name: assetName(), Size: 1234}})

	res, err := Run(Options{Current: "", Force: true, CheckOnly: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.UpdateAvailable || res.To != "2.0.0" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRunUpToDateSkipsHook(t *testing.T) {
	withReleaseServer(t, "v2.0.0", []ghAsset{{Name: assetName(), Size: 1234}})

	called := false
	res, err := Run(Options{
		Current: "2.0.0",
		BeforeInstall: func(*Result) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Fatal("BeforeInstall must not run when the binary is up to date")
	}
	if res.UpdateAvailable {
		t.Fatalf("unexpected update: %+v", res)
	}
}

func TestRunBeforeInstallAborts(t *testing.T) {
	withReleaseServer(t, "v2.0.0", []ghAsset{{Name: assetName(), Size: 1234}})

	called := false
	res, err := Run(Options{
		Current: "1.0.0",
		BeforeInstall: func(*Result) error {
			called = true
			return errors.New("no privileges")
		},
	})
	if !called {
		t.Fatal("BeforeInstall was not called")
	}
	if err == nil {
		t.Fatal("expected the BeforeInstall error")
	}
	if res != nil {
		t.Fatalf("expected a nil result, got %+v", res)
	}
}

func withReleaseServer(t *testing.T, tag string, assets []ghAsset) {
	t.Helper()

	oldBase := apiBaseURL
	t.Cleanup(func() { apiBaseURL = oldBase })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ghRelease{TagName: tag, Assets: assets}); err != nil {
			t.Errorf("encode release: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	apiBaseURL = server.URL
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
