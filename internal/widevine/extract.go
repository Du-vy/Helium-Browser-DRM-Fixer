package widevine

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractCRX extracts a Chromium CRX3 archive into destDir using pure Go.
func ExtractCRX(crxPath, destDir string) error {
	f, err := os.Open(crxPath)
	if err != nil {
		return fmt.Errorf("failed to open CRX file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat CRX file: %w", err)
	}
	fileSize := stat.Size()

	if fileSize < 12 {
		return errors.New("invalid CRX file: file too small")
	}

	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("failed to read CRX header: %w", err)
	}

	magic := string(header[:4])
	if magic != "Cr24" {
		return fmt.Errorf("invalid CRX file: invalid magic bytes (%q)", magic)
	}

	version := binary.LittleEndian.Uint32(header[4:8])
	if version != 3 {
		return fmt.Errorf("unsupported CRX version: %d (expected 3)", version)
	}

	headerLen := binary.LittleEndian.Uint32(header[8:12])
	zipOffset := int64(12 + headerLen)
	if zipOffset >= fileSize {
		return errors.New("invalid CRX file: header length exceeds file size")
	}

	zipSize := fileSize - zipOffset
	sectionReader := io.NewSectionReader(f, zipOffset, zipSize)

	zipReader, err := zip.NewReader(sectionReader, zipSize)
	if err != nil {
		return fmt.Errorf("failed to open embedded ZIP in CRX: %w", err)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	for _, file := range zipReader.File {
		cleanedPath := filepath.Clean(file.Name)
		if strings.HasPrefix(cleanedPath, "..") || filepath.IsAbs(cleanedPath) {
			continue
		}

		targetPath := filepath.Join(destDir, cleanedPath)

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
		}

		if err := extractZipFile(file, targetPath); err != nil {
			return fmt.Errorf("failed to extract file %s: %w", file.Name, err)
		}
	}

	return nil
}

func extractZipFile(file *zip.File, targetPath string) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}
