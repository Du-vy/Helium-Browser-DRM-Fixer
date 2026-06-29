# Helium Browser DRM Fixer

A cross-platform CLI tool that fixes DRM issues in the [Helium browser](https://helium.is/) by providing the Widevine CDM (Content Decryption Module).

Helium is a lightweight Chromium-based browser that does not bundle Google Widevine CDM, so DRM-protected content (YouTube, Crunchyroll, Spotify, etc.) won't play out of the box. This tool automates getting WidevineCdm and placing it where Helium expects it.

## How It Works

1. **Downloads WidevineCdm** from the latest Chrome release on GitHub (default).
2. If the download fails, **falls back** to copying WidevineCdm from a local Chrome installation.
3. Copies the CDM files into Helium's directory structure.
4. On **macOS**, re-signs the app bundle after modification.
5. On **Linux**, writes a component updater hint file so Chromium discovers the CDM.

> **Note:** This enables basic Widevine loading. Stricter services like Netflix or Amazon Prime Video may still fail due to additional requirements (browser identity, HDCP, Verified Media Path, etc.).

## Installation

### Download Pre-built Binary

Go to [Releases](https://github.com/Du-vy/helium-browser-drm-fixer/releases) and download the binary for your platform.

### Build from Source

```bash
git clone https://github.com/Du-vy/helium-browser-drm-fixer.git
cd helium-browser-drm-fixer
go build -o helium-drm-fixer .
```

**Requirements:** Go 1.22+, and on Windows [7-Zip](https://www.7-zip.org/) for the auto-download feature.

## Usage

```bash
# Fix DRM (auto-download WidevineCdm, no Chrome needed)
./helium-drm-fixer

# Check if fix is needed without applying changes
./helium-drm-fixer --check

# Show what would be done without making changes
./helium-drm-fixer --dry-run

# Enable verbose logging
./helium-drm-fixer --verbose

# Force re-download even if cached
./helium-drm-fixer --force-download

# Use custom paths
./helium-drm-fixer --chrome-path "C:\path\to\WidevineCdm" --helium-path "C:\path\to\Helium"
```

### Options

| Flag | Description |
|---|---|
| `--check` | Check if fix is needed without applying changes |
| `--dry-run` | Show what would be done without making changes |
| `--verbose` | Enable verbose logging |
| `--debug` | Alias for `--verbose` |
| `--chrome-path <path>` | Custom path to WidevineCdm directory |
| `--helium-path <path>` | Custom path to Helium target directory |
| `--force-download` | Force re-download even if a cached version exists |

## Platform Support

| Platform | Auto-download | Local Chrome fallback |
|---|---|---|
| **Windows** | Yes | Yes |
| **macOS** | No (install Chrome or use `--chrome-path`) | Yes |
| **Linux** | No (install Chrome or use `--chrome-path`) | Yes |

On macOS and Linux, if you don't have Chrome installed, pass the path manually:

```bash
# macOS
./helium-drm-fixer --chrome-path "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/133.0.6943.142/Libraries/WidevineCdm"

# Linux
./helium-drm-fixer --chrome-path "/opt/google/chrome/WidevineCdm"
```

## Troubleshooting

### "7-Zip not found" (Windows)
Install 7-Zip from [https://www.7-zip.org/](https://www.7-zip.org/) and make sure `7z.exe` is in your PATH.

### "Helium browser not found"
Ensure Helium is installed. Download from [https://github.com/imputnet/helium](https://github.com/imputnet/helium). On Linux, launch it at least once before running the tool.

### "Failed to copy WidevineCdm"
Make sure you have write permissions to the Helium application directory. On macOS you may need `sudo`.

### "Failed to re-sign Helium.app" (macOS)
Run with `sudo` or manually execute:
```bash
sudo codesign --force --deep --sign - "/Applications/Helium.app"
```

## Project Structure

```
├── main.go              # CLI entry point (flag parsing)
├── paths.go             # Chrome & Helium path detection (Windows, macOS, Linux)
├── download.go          # Chrome auto-download from GitHub + caching
├── extract.go           # 7-Zip extraction + recursive WidevineCdm search
├── utils.go             # Orchestration, directory copy, macOS re-sign
├── registry_windows.go  # Windows registry Chrome detection
├── registry_other.go    # Stub for non-Windows
├── go.mod / go.sum      # Dependencies
└── LICENSE
```

## Credits

- [Helium Browser](https://github.com/imputnet/helium) — the browser this tool fixes
- [Chrome installer repo](https://github.com/Bush2021/chrome_installer) — source for Widevine CDM downloads

## License

MIT
