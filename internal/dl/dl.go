// Package dl provides shared HTTP download helpers with progress reporting.
package dl

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// downloadTimeout bounds a single asset download so a stalled connection can
// never hang the caller forever.
const downloadTimeout = 10 * time.Minute

// Download fetches the given URL and writes it to destPath, showing a
// lightweight progress bar on stdout. The data is written to a temporary
// ".part" file first and renamed into place, so destPath is never left
// truncated on a failed download.
func Download(url, destPath string) (err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "helium-drm-fixer")

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	total := resp.ContentLength

	tmpPath := destPath + ".part"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

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

	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	closed = true

	// os.Rename cannot overwrite an existing file on Windows, and callers may
	// be refreshing a cached copy.
	_ = os.Remove(destPath)
	if err = os.Rename(tmpPath, destPath); err != nil {
		return err
	}

	return nil
}

// SHA256 computes the hex-encoded SHA-256 checksum of the file at path.
func SHA256(path string) (string, error) {
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
