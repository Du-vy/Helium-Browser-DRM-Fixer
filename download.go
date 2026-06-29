package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	chromeRepo    = "Bush2021/chrome_installer"
	githubAPIURL  = "https://api.github.com/repos/" + chromeRepo + "/releases/latest"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	HTMLURL string        `json:"html_url"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type versionCache struct {
	Tag          string `json:"tag"`
	Name         string `json:"name"`
	DownloadedAt int64  `json:"downloadedAt"`
	Checksum     string `json:"checksum,omitempty"`
}

func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", githubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "helium-drm-fixer")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return &release, nil
}

func findMatchingAsset(release *githubRelease) (*githubAsset, error) {
	pattern := getChromeAssetPattern()

	for _, asset := range release.Assets {
		if pattern.MatchString(asset.Name) {
			return &asset, nil
		}
	}

	return nil, fmt.Errorf("could not find Chrome asset matching pattern for %s/%s (pattern: %s)",
		runtime.GOOS, runtime.GOARCH, pattern.String())
}

func getChromeAssetPattern() *regexp.Regexp {
	var builder strings.Builder

	if runtime.GOOS == "windows" {
		if runtime.GOARCH == "arm64" {
			builder.WriteString(`^arm64_`)
		} else {
			builder.WriteString(`^x64_`)
		}
		builder.WriteString(`[\d.]+_chrome_installer_uncompressed\.exe$`)
	} else {
		panic("Chrome download is only supported on Windows")
	}

	return regexp.MustCompile(builder.String())
}

func checkCachedVersion(exePath, versionFile string, release *githubRelease, forceDownload bool) bool {
	if forceDownload {
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

	if cache.Tag != release.TagName || cache.Checksum == "" {
		return false
	}

	currentChecksum, err := sha256File(exePath)
	if err != nil || currentChecksum != cache.Checksum {
		return false
	}

	return true
}

func downloadFile(url, destPath string, log *logger) error {
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
	if total <= 0 {
		return errors.New("Content-Length header not found in response")
	}

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

func downloadAndExtractChrome(log *logger, forceDownload bool) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("automatic Chrome download is only supported on Windows")
	}

	log.info("Chrome not found locally. Downloading from GitHub...")

	release, err := fetchLatestRelease()
	if err != nil {
		log.error("Failed to connect to GitHub API: %v", err)
		return "", err
	}

	asset, err := findMatchingAsset(release)
	if err != nil {
		return "", err
	}

	tempDir := filepath.Join(os.TempDir(), "helium-drm-download")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", err
	}

	exePath := filepath.Join(tempDir, asset.Name)
	versionFile := filepath.Join(tempDir, "version.json")

	if !checkCachedVersion(exePath, versionFile, release, forceDownload) {
		fmt.Printf("  Downloading %s (%s)...\n", asset.Name, release.TagName)
		log.info("Downloading %s (%s)...", asset.Name, release.TagName)

		if err := downloadFile(asset.BrowserDownloadURL, exePath, log); err != nil {
			log.error("Failed to download Chrome installer: %v", err)
			return "", err
		}

		checksum, err := sha256File(exePath)
		if err != nil {
			return "", err
		}

		cache := versionCache{
			Tag:          release.TagName,
			Name:         asset.Name,
			DownloadedAt: time.Now().Unix(),
			Checksum:     checksum,
		}
		cacheData, _ := json.Marshal(cache)
		os.WriteFile(versionFile, cacheData, 0644)
	} else {
		fmt.Printf("  Using cached %s (%s)\n", asset.Name, release.TagName)
	}

	fmt.Print("  Extracting WidevineCdm...\n")
	log.info("Extracting WidevineCdm...")

	if err := extractArchive(exePath, tempDir); err != nil {
		return "", err
	}

	log.info("Searching for WidevineCdm in extracted files...")
	widevinePath, err := findWidevineInExtracted(tempDir)
	if err != nil {
		log.error("Could not find WidevineCdm in downloaded Chrome")
		return "", err
	}

	log.info("Chrome downloaded and extracted successfully.")
	return widevinePath, nil
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
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)

	fmt.Printf("\r  Downloading: [%s] %d%% (%.1f MB / %.1f MB)", bar, percentage, downloadedMB, totalMB)
}
