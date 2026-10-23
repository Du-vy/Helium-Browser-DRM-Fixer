// Package updater implements self-update from GitHub releases: it compares the
// running version against the latest published release, downloads the matching
// platform asset, verifies its SHA-256 checksum and atomically replaces the
// running executable (restarting the background service when applicable).
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"helium-drm-fixer/internal/dl"
	"helium-drm-fixer/internal/fixer"
	"helium-drm-fixer/internal/service"
)

// DefaultRepo is the GitHub repository (owner/name) releases are fetched from.
const DefaultRepo = "Du-vy/Helium-Browser-DRM-Fixer"

// apiBaseURL is a variable so tests can point the client at an httptest server.
var apiBaseURL = "https://api.github.com"

// ErrAborted is returned when the user declines the confirmation prompt.
var ErrAborted = errors.New("update aborted")

// Options configures a self-update run.
type Options struct {
	Repo       string // GitHub repository as "owner/name"
	Current    string // currently installed version, without a leading "v"
	Force      bool   // update even when versions match
	CheckOnly  bool   // report availability without downloading or replacing
	AssumeYes  bool   // skip the interactive confirmation prompt
	SkipVerify bool   // install without checking the release SHA256SUMS file
	CacheDir   string // optional download cache directory (tests/advanced use)

	// BeforeInstall runs once an update has been confirmed but before anything
	// is downloaded. Returning an error aborts the update. The CLI uses it to
	// require elevation only when an update is actually available.
	BeforeInstall func(*Result) error
}

// Result describes the outcome of a self-update run.
type Result struct {
	UpdateAvailable  bool   // a newer release was found (or forced)
	Updated          bool   // the executable was replaced successfully
	From             string // version before the update
	To               string // version after the update (latest release)
	AssetName        string // release asset used for the update
	AssetSize        int64  // asset size in bytes
	ChecksumVerified bool   // asset matched the published SHA256SUMS entry
	ServiceRestarted bool   // background service was restarted after replacement
	BackupPath       string // previous binary kept for manual rollback
	Warning          string // non-fatal problem worth surfacing to the user
}

type ghAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Name    string    `json:"name"`
	Assets  []ghAsset `json:"assets"`
}

// Run performs the full self-update flow and reports what happened.
func Run(opts Options) (*Result, error) {
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.Current == "" && !opts.Force {
		return nil, errors.New("current version is unknown (the binary was built without version flags); re-run with --force to install the latest release anyway")
	}

	release, err := fetchLatestRelease(opts.Repo)
	if err != nil {
		return nil, err
	}

	asset, err := pickAsset(release)
	if err != nil {
		return nil, err
	}

	latest := normalizeVersion(release.TagName)
	res := &Result{
		From:      normalizeVersion(opts.Current),
		To:        latest,
		AssetName: asset.Name,
		AssetSize: asset.Size,
	}

	updateNeeded := opts.Force
	if !updateNeeded {
		cmp, err := compareSemver(opts.Current, latest)
		if err != nil {
			return nil, fmt.Errorf("could not compare versions %q and %q: %w", opts.Current, latest, err)
		}
		updateNeeded = cmp < 0
	}

	if !updateNeeded {
		return res, nil
	}
	res.UpdateAvailable = true

	if opts.CheckOnly {
		return res, nil
	}

	printUpdateSummary(opts.Current, release, asset)

	if opts.BeforeInstall != nil {
		if err := opts.BeforeInstall(res); err != nil {
			return nil, err
		}
	}

	exePath, err := selfPath()
	if err != nil {
		return nil, fmt.Errorf("could not locate the current executable: %w", err)
	}

	// Guard against "go run" / "go build" into go-build temp dirs, where
	// replacing the executable is pointless and misleading.
	if strings.Contains(strings.ToLower(exePath), "go-build") {
		return nil, errors.New("the current executable lives in a temporary go-build directory (are you running via 'go run'?); build a binary or download an official release instead")
	}

	// Resolve the service state before downloading so a broken service setup
	// aborts the update instead of silently replacing the binary underneath a
	// running service.
	wasRunning, err := service.IsRunning()
	if err != nil {
		return nil, fmt.Errorf("could not determine whether the background service is running; stop it with 'helium-drm-fixer service stop' and retry: %w", err)
	}

	if !opts.AssumeYes {
		fmt.Print("\n  Install update now? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			return res, ErrAborted
		}
	}
	fmt.Println()

	cacheDir, err := getCacheDir(opts.CacheDir)
	if err != nil {
		return nil, err
	}

	assetPath, err := downloadAsset(release, asset, cacheDir)
	if err != nil {
		return nil, err
	}

	if opts.SkipVerify {
		fmt.Printf("%s  Warning: skipping SHA-256 verification (--skip-verify)%s\n", fixer.ColorYellow, fixer.ColorReset)
	} else {
		if err := verifyChecksum(release, asset, assetPath, cacheDir); err != nil {
			return nil, err
		}
		res.ChecksumVerified = true
	}

	if wasRunning {
		fmt.Println("  Stopping background service...")
		if err := service.Stop(); err != nil {
			return nil, fmt.Errorf("failed to stop the background service before updating (is the terminal elevated?): %w", err)
		}
	}

	backupPath, err := replaceBinary(assetPath, exePath)
	if err != nil {
		if wasRunning {
			if startErr := service.Start(); startErr != nil {
				return nil, fmt.Errorf("%w (additionally, the background service failed to restart: %v)", err, startErr)
			}
		}
		return nil, err
	}
	res.BackupPath = backupPath
	_ = os.Remove(assetPath)

	if wasRunning {
		fmt.Println("  Restarting background service...")
		if err := service.Start(); err != nil {
			res.Updated = true
			res.Warning = fmt.Sprintf("update installed but the background service failed to restart: %v", err)
			return res, nil
		}
		res.ServiceRestarted = true
	}

	res.Updated = true
	return res, nil
}

func printUpdateSummary(current string, release *ghRelease, asset *ghAsset) {
	fmt.Printf("  Current version: %s\n", versionLabel(current))
	fmt.Printf("  Latest release:  %s%s%s", fixer.ColorBold, versionLabel(release.TagName), fixer.ColorReset)
	if release.Name != "" && release.Name != release.TagName {
		fmt.Printf("%s  (%s)%s", fixer.ColorDim, release.Name, fixer.ColorReset)
	}
	fmt.Println()
	fmt.Printf("  Asset:           %s (%.1f MB)\n", asset.Name, float64(asset.Size)/(1024*1024))
}

func versionLabel(v string) string {
	normalized := normalizeVersion(v)
	if normalized == "" {
		return "unknown"
	}
	return "v" + normalized
}

// fetchLatestRelease queries the GitHub releases API for the newest stable
// release of the given repository.
func fetchLatestRelease(repo string) (*ghRelease, error) {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")
	if repo == "" {
		return nil, errors.New("empty GitHub repository (expected owner/name)")
	}

	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(apiBaseURL, "/"), repo)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "helium-drm-fixer")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach GitHub Releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found for %s (check the --repo value)", repo)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GitHub API rate limit exceeded or access denied (status %d); try again later", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub Releases API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read GitHub response: %w", err)
	}

	var release ghRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}
	if release.TagName == "" {
		return nil, errors.New("GitHub response contained no release tag")
	}

	return &release, nil
}

// pickAsset returns the release asset matching the current platform.
func pickAsset(release *ghRelease) (*ghAsset, error) {
	want := assetName()
	for i := range release.Assets {
		if strings.EqualFold(release.Assets[i].Name, want) {
			return &release.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("release %s has no asset for %s/%s (missing %q)", release.TagName, runtime.GOOS, runtime.GOARCH, want)
}

func assetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("helium-drm-fixer-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

func findAsset(release *ghRelease, name string) *ghAsset {
	for i := range release.Assets {
		if strings.EqualFold(release.Assets[i].Name, name) {
			return &release.Assets[i]
		}
	}
	return nil
}

// downloadAsset fetches the release asset into the local update cache.
func downloadAsset(release *ghRelease, asset *ghAsset, cacheDir string) (string, error) {
	assetPath := filepath.Join(cacheDir, asset.Name)
	if err := os.Remove(assetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	fmt.Printf("  Downloading %s (%s)...\n", versionLabel(release.TagName), asset.Name)
	if err := dl.Download(asset.BrowserDownloadURL, assetPath); err != nil {
		return "", fmt.Errorf("failed to download %s: %w", asset.Name, err)
	}

	return assetPath, nil
}

// verifyChecksum validates the downloaded asset against the SHA256SUMS file
// published with the release. Verification is mandatory: a release without a
// usable checksum file is rejected so an unverified binary is never installed.
func verifyChecksum(release *ghRelease, asset *ghAsset, assetPath, cacheDir string) error {
	sums := findAsset(release, "SHA256SUMS")
	if sums == nil {
		return fmt.Errorf("release %s does not publish a SHA256SUMS file; refusing to install an unverified binary (re-run with --skip-verify to override)", release.TagName)
	}

	sumsPath := filepath.Join(cacheDir, "SHA256SUMS")
	if err := os.Remove(sumsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Println("  Downloading SHA256SUMS...")
	if err := dl.Download(sums.BrowserDownloadURL, sumsPath); err != nil {
		return fmt.Errorf("could not download SHA256SUMS: %w", err)
	}

	expected, ok := lookupChecksum(sumsPath, asset.Name)
	if !ok {
		return fmt.Errorf("SHA256SUMS has no entry for %s", asset.Name)
	}

	actual, err := dl.SHA256(assetPath)
	if err != nil {
		return fmt.Errorf("failed to compute checksum of %s: %w", asset.Name, err)
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset.Name, expected, actual)
	}

	return nil
}

func getCacheDir(override string) (string, error) {
	if override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", err
		}
		return override, nil
	}

	if base, err := os.UserCacheDir(); err == nil {
		p := filepath.Join(base, "helium-drm-fixer", "update")
		if mkErr := os.MkdirAll(p, 0o755); mkErr == nil {
			return p, nil
		}
	}

	p := filepath.Join(os.TempDir(), "helium-drm-fixer-update")
	if mkErr := os.MkdirAll(p, 0o755); mkErr != nil {
		return "", mkErr
	}
	return p, nil
}

// lookupChecksum parses a sha256sum-style file ("<hash>  <filename>" per line)
// and returns the checksum for the given asset name.
func lookupChecksum(sumsPath, assetName string) (string, bool) {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", false
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if strings.EqualFold(name, assetName) {
			return fields[0], true
		}
	}
	return "", false
}

// selfPath resolves the real path of the currently running executable.
func selfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if link, err := filepath.EvalSymlinks(exe); err == nil {
		exe = link
	}
	return exe, nil
}

// replaceBinary swaps the running executable for the downloaded asset. The new
// binary is staged next to the target first, then the old binary is moved aside
// and the staged file renamed into place, so a failed copy never leaves a
// truncated executable. The previous binary is kept as "<binary>.old" until the
// next update so it can be restored manually.
func replaceBinary(assetPath, exePath string) (string, error) {
	return replaceBinaryWith(assetPath, exePath, os.Rename)
}

// replaceBinaryWith is replaceBinary with an injectable rename function so the
// rollback path can be exercised in tests.
func replaceBinaryWith(assetPath, exePath string, renameFn func(string, string) error) (string, error) {
	perm := os.FileMode(0o755)
	if info, err := os.Stat(exePath); err == nil && info.Mode().Perm() != 0 {
		perm = info.Mode().Perm()
	}

	newPath := exePath + ".new"
	_ = os.Remove(newPath)
	if err := copyFile(assetPath, newPath, perm); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("failed to stage the new executable: %w", err)
	}

	// Only drop the previous backup once the new binary is fully staged, so a
	// failed copy cannot lose the last known-good copy.
	oldPath := exePath + ".old"
	if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		// A running image (or antivirus) can keep the previous backup locked.
		// Keep it and use a unique backup name instead of blocking every
		// future update.
		oldPath = fmt.Sprintf("%s.old.%d", exePath, time.Now().UnixNano())
		fmt.Printf("%s  Note: could not remove the previous backup; keeping it as %s%s\n", fixer.ColorYellow, oldPath, fixer.ColorReset)
	}

	if err := renameFn(exePath, oldPath); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("cannot move the current executable aside (try running with administrator privileges): %w", err)
	}

	if err := renameFn(newPath, exePath); err != nil {
		if rbErr := renameFn(oldPath, exePath); rbErr != nil {
			return "", fmt.Errorf("failed to install the update (%v) and rolling back also failed (%v); old binary: %s, new binary: %s", err, rbErr, oldPath, newPath)
		}
		_ = os.Remove(newPath)
		return "", fmt.Errorf("failed to install the update (original executable restored): %w", err)
	}

	return oldPath, nil
}

func copyFile(src, dest string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// normalizeVersion trims whitespace and any leading "v"/"V" prefix.
func normalizeVersion(v string) string {
	return strings.TrimLeft(strings.TrimSpace(v), "vV")
}

type semver struct {
	major, minor, patch int
	pre                 string // prerelease identifiers without the leading "-", "" when absent
}

// parseSemver parses "x.y.z[-pre][+build]" (a leading "v" is ignored, missing
// minor or patch parts default to zero, build metadata is discarded).
func parseSemver(v string) (semver, error) {
	s := normalizeVersion(v)

	// Build metadata never affects precedence.
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		if idx == len(s)-1 {
			return semver{}, fmt.Errorf("invalid semantic version %q", v)
		}
		s = s[:idx]
	}

	core := s
	pre := ""
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		core = s[:idx]
		pre = s[idx+1:]
		if pre == "" {
			return semver{}, fmt.Errorf("invalid semantic version %q", v)
		}
	}

	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semver{}, fmt.Errorf("invalid semantic version %q", v)
	}

	var nums [3]int
	for i, p := range parts {
		if p == "" {
			return semver{}, fmt.Errorf("invalid semantic version %q", v)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("invalid semantic version %q", v)
		}
		nums[i] = n
	}

	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, nil
}

// compareSemver returns -1, 0 or 1 when a is older, equal or newer than b.
// Prerelease suffixes (e.g. "2.0.0-beta") rank below their final release.
func compareSemver(a, b string) (int, error) {
	pa, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}

	if pa.major != pb.major {
		if pa.major < pb.major {
			return -1, nil
		}
		return 1, nil
	}
	if pa.minor != pb.minor {
		if pa.minor < pb.minor {
			return -1, nil
		}
		return 1, nil
	}
	if pa.patch != pb.patch {
		if pa.patch < pb.patch {
			return -1, nil
		}
		return 1, nil
	}

	switch {
	case pa.pre == "" && pb.pre == "":
		return 0, nil
	case pa.pre == "":
		return 1, nil
	case pb.pre == "":
		return -1, nil
	default:
		return comparePrerelease(pa.pre, pb.pre), nil
	}
}

// comparePrerelease compares dot-separated prerelease identifiers the way
// semantic versioning prescribes: purely numeric identifiers compare
// numerically, non-numeric identifiers compare lexically.
func comparePrerelease(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		na, aErr := strconv.Atoi(partsA[i])
		nb, bErr := strconv.Atoi(partsB[i])

		switch {
		case aErr == nil && bErr == nil:
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
		case aErr == nil: // numeric identifiers rank below non-numeric ones
			return -1
		case bErr == nil:
			return 1
		default:
			if partsA[i] > partsB[i] {
				return 1
			}
			if partsA[i] < partsB[i] {
				return -1
			}
		}
	}

	switch {
	case len(partsA) < len(partsB):
		return -1
	case len(partsA) > len(partsB):
		return 1
	default:
		return 0
	}
}
