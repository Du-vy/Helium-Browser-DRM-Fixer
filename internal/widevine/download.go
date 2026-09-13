package widevine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	widevineAppID   = "oimompecagnajdejgnnjijobebaeigek"
	googleUpdateURL = "https://update.googleapis.com/service/update2/json"
	updaterVersion  = "999.0.0.0" // High version to ensure future-proofing against minimum version checks
)

// Logger defines the logging methods expected by the widevine package.
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

type updateResponse struct {
	Response struct {
		Apps []struct {
			Appid       string `json:"appid"`
			Status      string `json:"status"`
			UpdateCheck struct {
				Status      string `json:"status"`
				NextVersion string `json:"nextversion"`
				Pipelines   []struct {
					Operations []struct {
						Type string `json:"type"`
						Out  *struct {
							Sha256 string `json:"sha256"`
						} `json:"out,omitempty"`
						Urls []struct {
							Url string `json:"url"`
						} `json:"urls"`
					} `json:"operations"`
				} `json:"pipelines"`
			} `json:"updatecheck"`
		} `json:"apps"`
	} `json:"response"`
}

type releaseInfo struct {
	Version  string
	URL      string
	SHA256   string
	FileName string
}

type versionCache struct {
	Version      string `json:"version"`
	FileName     string `json:"fileName"`
	DownloadedAt int64  `json:"downloadedAt"`
	Checksum     string `json:"checksum,omitempty"`
}

func getPlatformAndArch() (string, string, error) {
	var platform string
	switch runtime.GOOS {
	case "windows":
		platform = "win"
	case "darwin":
		platform = "mac"
	case "linux":
		platform = "linux"
	default:
		return "", "", fmt.Errorf("unsupported OS for automatic download: %s", runtime.GOOS)
	}

	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	case "386":
		arch = "x86"
	default:
		return "", "", fmt.Errorf("unsupported architecture for automatic download: %s", runtime.GOARCH)
	}

	return platform, arch, nil
}

func fetchLatestRelease(platform, arch string) (*releaseInfo, error) {
	jsonPayload := fmt.Sprintf(`{
		"request": {
			"@os": "",
			"@updater": "",
			"acceptformat": "crx3,download,puff,run,xz,zucc",
			"apps": [
				{
					"appid": "%s",
					"installsource": "ondemand",
					"updatecheck": {},
					"version": "1.4.9.1088"
				}
			],
			"dedup": "cr",
			"ismachine": false,
			"arch": "%s",
			"os": {
				"arch": "%s",
				"platform": "%s"
			},
			"protocol": "4.0",
			"updaterversion": "%s"
		}
	}`, widevineAppID, arch, arch, platform, updaterVersion)

	req, err := http.NewRequest("POST", googleUpdateURL, bytes.NewBufferString(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Google Update API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Update API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	bodyStr := strings.TrimPrefix(string(body), ")]}'\n")
	bodyStr = strings.TrimPrefix(bodyStr, ")]}'")

	var res updateResponse
	if err := json.Unmarshal([]byte(bodyStr), &res); err != nil {
		return nil, fmt.Errorf("failed to decode Google Update response: %w", err)
	}

	if len(res.Response.Apps) == 0 {
		return nil, errors.New("no applications found in Google Update response")
	}

	app := res.Response.Apps[0]
	if app.Status != "ok" || app.UpdateCheck.Status != "ok" {
		return nil, fmt.Errorf("Google Update API returned non-ok status (app: %s, update: %s)", app.Status, app.UpdateCheck.Status)
	}

	var downloadURL, expectedSha256 string
	for _, pipe := range app.UpdateCheck.Pipelines {
		for _, op := range pipe.Operations {
			if op.Type == "download" {
				if op.Out != nil && op.Out.Sha256 != "" {
					expectedSha256 = op.Out.Sha256
				}
				for _, u := range op.Urls {
					if strings.HasPrefix(u.Url, "https://") {
						downloadURL = u.Url
						break
					}
				}
				if downloadURL == "" && len(op.Urls) > 0 {
					downloadURL = op.Urls[0].Url
				}
			}
		}
	}

	if downloadURL == "" {
		return nil, errors.New("no download URL found in Google Update response")
	}

	urlParts := strings.Split(downloadURL, "/")
	fileName := urlParts[len(urlParts)-1]
	if !strings.HasSuffix(fileName, ".crx3") {
		fileName = fmt.Sprintf("widevine_%s_%s_%s.crx3", platform, arch, app.UpdateCheck.NextVersion)
	}

	return &releaseInfo{
		Version:  app.UpdateCheck.NextVersion,
		URL:      downloadURL,
		SHA256:   expectedSha256,
		FileName: fileName,
	}, nil
}

func getCacheDir() string {
	cacheDir, err := os.UserCacheDir()
	if err == nil {
		p := filepath.Join(cacheDir, "helium-drm-fixer")
		if err := os.MkdirAll(p, 0755); err == nil {
			return p
		}
	}
	p := filepath.Join(os.TempDir(), "helium-drm-cache")
	_ = os.MkdirAll(p, 0755)
	return p
}

func checkCachedVersion(crxPath, versionFile, expectedVersion, expectedSha256 string, forceDownload bool) bool {
	if forceDownload {
		return false
	}

	if _, err := os.Stat(crxPath); err != nil {
		return false
	}

	vfData, err := os.ReadFile(versionFile)
	if err != nil {
		return false
	}

	var cache versionCache
	if err := json.Unmarshal(vfData, &cache); err != nil {
		return false
	}

	if cache.Version != expectedVersion {
		return false
	}

	if expectedSha256 != "" {
		if cache.Checksum == "" || !strings.EqualFold(cache.Checksum, expectedSha256) {
			return false
		}
		currentChecksum, err := sha256File(crxPath)
		if err != nil || !strings.EqualFold(currentChecksum, expectedSha256) {
			return false
		}
	}

	return true
}

func downloadFile(url, destPath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "helium-drm-fixer")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	total := resp.ContentLength

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var downloaded int64
	buf := make([]byte, 32*1024)
	lastLog := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)

			if time.Since(lastLog) > 200*time.Millisecond {
				printProgress(downloaded, total)
				lastLog = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	printProgress(downloaded, total)
	fmt.Print("\n")
	return nil
}

// Get fetches Widevine CDM from Google Update API, checks cache, verifies checksum, and extracts it.
func Get(log Logger, forceDownload bool) (string, error) {
	platform, arch, err := getPlatformAndArch()
	if err != nil {
		return "", err
	}

	log.Info("Detected platform: %s, arch: %s", platform, arch)
	log.Info("Querying Google Component Updater API...")

	release, err := fetchLatestRelease(platform, arch)
	if err != nil {
		log.Error("Failed to query Google Component Updater API: %v", err)
		return "", err
	}

	log.Info("Latest Widevine version available: %s", release.Version)

	cacheDir := getCacheDir()
	crxPath := filepath.Join(cacheDir, release.FileName)
	versionFile := filepath.Join(cacheDir, "version.json")
	extractedDir := filepath.Join(cacheDir, "WidevineCdm")

	if !checkCachedVersion(crxPath, versionFile, release.Version, release.SHA256, forceDownload) {
		fmt.Printf("  Downloading WidevineCdm %s (%s)...\n", release.Version, release.FileName)
		log.Info("Downloading %s from %s...", release.FileName, release.URL)

		if err := downloadFile(release.URL, crxPath); err != nil {
			log.Error("Failed to download Widevine CDM: %v", err)
			return "", err
		}

		if release.SHA256 != "" {
			checksum, err := sha256File(crxPath)
			if err != nil {
				return "", err
			}
			if !strings.EqualFold(checksum, release.SHA256) {
				return "", fmt.Errorf("checksum mismatch: expected %s, got %s", release.SHA256, checksum)
			}
			log.Info("Checksum verified: %s", checksum)
		}

		cache := versionCache{
			Version:      release.Version,
			FileName:     release.FileName,
			DownloadedAt: time.Now().Unix(),
			Checksum:     release.SHA256,
		}
		cacheData, _ := json.Marshal(cache)
		_ = os.WriteFile(versionFile, cacheData, 0644)

		fmt.Print("  Extracting WidevineCdm...\n")
		log.Info("Extracting CRX3 to %s...", extractedDir)
		if err := ExtractCRX(crxPath, extractedDir); err != nil {
			log.Error("Failed to extract CRX: %v", err)
			return "", err
		}
	} else {
		fmt.Printf("  Using cached WidevineCdm %s\n", release.Version)
		if info, err := os.Stat(filepath.Join(extractedDir, "manifest.json")); err != nil || info.IsDir() {
			log.Info("Extracted directory missing or incomplete, extracting CRX3...")
			if err := ExtractCRX(crxPath, extractedDir); err != nil {
				return "", err
			}
		}
	}

	return extractedDir, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func printProgress(downloaded, total int64) {
	percentage := int64(0)
	if total > 0 {
		percentage = downloaded * 100 / total
	}
	downloadedMB := float64(downloaded) / (1024 * 1024)
	totalMB := float64(total) / (1024 * 1024)

	barLen := 30
	filled := int(percentage * int64(barLen) / 100)
	if filled > barLen {
		filled = barLen
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)

	if total > 0 {
		fmt.Printf("\r  Downloading: [%s] %d%% (%.1f MB / %.1f MB)", bar, percentage, downloadedMB, totalMB)
	} else {
		fmt.Printf("\r  Downloading: [%s] (%.1f MB)", bar, downloadedMB)
	}
}
