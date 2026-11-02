package service

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kardianos/service"
)

func TestPlatformInstallDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	data := filepath.Join(t.TempDir(), "data")
	programFiles := filepath.Join(t.TempDir(), "Program Files")
	cases := []struct {
		name, goos, xdg, programFiles, want string
		wantErr                             bool
	}{
		{"macOS", "darwin", data, "", filepath.Join(home, "Library", "Application Support", "helium-drm-fixer", "bin"), false},
		{"Linux", "linux", "", "", filepath.Join(home, ".local", "share", "helium-drm-fixer", "bin"), false},
		{"Linux XDG", "linux", data, "", filepath.Join(data, "helium-drm-fixer", "bin"), false},
		{"relative XDG ignored", "linux", "relative", "", filepath.Join(home, ".local", "share", "helium-drm-fixer", "bin"), false},
		{"Windows", "windows", "", programFiles, filepath.Join(programFiles, "Helium DRM Fixer"), false},
		{"Windows missing ProgramFiles", "windows", "", "", "", true},
		{"Windows relative ProgramFiles", "windows", "", "relative", "", true},
		{"unsupported OS", "unsupported", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "XDG_DATA_HOME":
					return tc.xdg
				case "ProgramFiles":
					return tc.programFiles
				}
				return ""
			}
			got, err := platformInstallDir(tc.goos, getenv, func() (string, error) { return home, nil })
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("platformInstallDir = %q, %v; want %q, error=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestPlatformInstallDirHomeError(t *testing.T) {
	want := errors.New("no home")
	for _, goos := range []string{"darwin", "linux"} {
		_, err := platformInstallDir(goos, func(string) string { return "" }, func() (string, error) { return "", want })
		if !errors.Is(err, want) {
			t.Fatalf("%s: error = %v; want %v", goos, err, want)
		}
	}
}

func TestInstallFrom(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	source := filepath.Join(dir, "downloaded-release")
	writeExecutable(t, source, "binary contents")
	installDir := filepath.Join("Application Support", "Helium Fixer")
	opts := Options{
		InstallDir:    installDir,
		Interval:      30 * time.Minute,
		HeliumPath:    "Helium App",
		ChromePath:    "Chrome Widevine",
		ForceDownload: true,
		Verbose:       true,
	}
	var registered *service.Config
	path, err := installFrom(opts, source, func(config *service.Config) error {
		registered = config
		// The complete executable must be available before registration.
		assertFileContents(t, config.Executable, "binary contents")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(dir, installDir, executableName())
	if path != wantPath || registered == nil || registered.Executable != wantPath {
		t.Fatalf("installed path = %q, config = %+v; want %q", path, registered, wantPath)
	}
	wantArgs := []string{
		"service", "run", "--interval", "30m0s",
		"--helium-path", filepath.Join(dir, "Helium App"),
		"--chrome-path", filepath.Join(dir, "Chrome Widevine"),
		"--force-download", "--verbose",
	}
	if !reflect.DeepEqual(registered.Arguments, wantArgs) {
		t.Fatalf("arguments = %q; want %q", registered.Arguments, wantArgs)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if registered.Option["UserService"] != true {
			t.Fatal("installation must remain a user service")
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0755 {
			t.Fatalf("installed executable permissions: %v, %v", info, err)
		}
	}
	// The original download can be removed without affecting the service copy.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, "binary contents")
}

func TestInstallFromDefaultDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("ProgramFiles", filepath.Join(dir, "Program Files"))
	source := filepath.Join(dir, "download")
	writeExecutable(t, source, "binary")
	wantDir, err := defaultInstallDir()
	if err != nil {
		t.Fatal(err)
	}
	path, err := installFrom(Options{}, source, func(*service.Config) error { return nil })
	if err != nil || path != filepath.Join(wantDir, executableName()) {
		t.Fatalf("installed path = %q, %v; want directory %q", path, err, wantDir)
	}
}

func TestInstallFromRegistrationFailure(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "download")
	writeExecutable(t, source, "binary")
	installDir := filepath.Join(dir, "install")
	wantErr := errors.New("registration failed")
	_, err := installFrom(Options{InstallDir: installDir}, source, func(*service.Config) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v; want %v", err, wantErr)
	}
	if _, err := os.Stat(filepath.Join(installDir, executableName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed installation left a copy behind: %v", err)
	}
	assertFileContents(t, source, "binary")
}

func TestInstallFromSameExecutable(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure preserves original"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, executableName())
			writeExecutable(t, source, "installed binary")
			path, err := installFrom(Options{InstallDir: dir}, source, func(*service.Config) error {
				if fail {
					return errors.New("registration failed")
				}
				return nil
			})
			if (err != nil) != fail || (!fail && path != source) {
				t.Fatalf("installFrom = %q, %v", path, err)
			}
			assertFileContents(t, source, "installed binary")
		})
	}
}

func TestInstallFromCopyFailure(t *testing.T) {
	for _, tc := range []string{"missing source", "directory source", "existing destination", "directory destination", "blocked parent"} {
		t.Run(tc, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "download")
			installDir := filepath.Join(dir, "install")
			destination := filepath.Join(installDir, executableName())
			if tc == "directory source" {
				if err := os.Mkdir(source, 0755); err != nil {
					t.Fatal(err)
				}
			} else if tc != "missing source" {
				writeExecutable(t, source, "new binary")
			}
			switch tc {
			case "existing destination":
				if err := os.Mkdir(installDir, 0755); err != nil {
					t.Fatal(err)
				}
				writeExecutable(t, destination, "existing file")
			case "directory destination":
				if err := os.MkdirAll(destination, 0755); err != nil {
					t.Fatal(err)
				}
			case "blocked parent":
				writeExecutable(t, installDir, "unrelated file")
			}
			_, err := installFrom(Options{InstallDir: installDir}, source, func(*service.Config) error {
				t.Fatal("registration must not happen if copying fails")
				return nil
			})
			if err == nil {
				t.Fatal("expected installation error")
			}
			if tc == "existing destination" {
				assertFileContents(t, destination, "existing file")
				if !strings.Contains(err.Error(), "already exists") {
					t.Fatalf("missing actionable error: %v", err)
				}
			}
			if tc == "blocked parent" {
				assertFileContents(t, installDir, "unrelated file")
			}
		})
	}
}

func TestInstallFromDoesNotReuseSymlinkToDownload(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "download")
	writeExecutable(t, source, "binary")
	installDir := filepath.Join(dir, "install")
	if err := os.Mkdir(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(installDir, executableName())
	if err := os.Symlink(source, destination); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := installFrom(Options{InstallDir: installDir}, source, func(*service.Config) error {
		t.Fatal("must not register a symlink that still depends on Downloads")
		return nil
	})
	if err == nil {
		t.Fatal("expected existing symlink to be rejected")
	}
	assertFileContents(t, source, "binary")
	assertFileContents(t, destination, "binary")
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "helium-drm-fixer.exe"
	}
	return "helium-drm-fixer"
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatal(err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("file %s = %q, %v; want %q", path, got, err, want)
	}
}
