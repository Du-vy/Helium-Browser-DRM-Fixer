# Helium Browser DRM Fixer

A cross-platform CLI tool that fixes DRM issues in the [Helium browser](https://helium.is/) by providing the Widevine CDM (Content Decryption Module).

Helium is a lightweight Chromium-based browser that does not bundle Google Widevine CDM, so DRM-protected content (YouTube, Crunchyroll, Spotify, etc.) won't play out of the box. This tool automates getting WidevineCdm and placing it where Helium expects it.

## How It Works

1. **Downloads WidevineCdm** directly from Google's official Component Updater API (default).
2. Extracts the official `.crx3` package using pure Go (no external tools like 7-Zip required).
3. If the download fails, **falls back** to copying WidevineCdm from a local Chrome installation.
4. Copies the CDM files into Helium's directory structure.
5. On **macOS**, re-signs the app bundle after modification.
6. On **Linux**, writes a component updater hint file so Chromium discovers the CDM.

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

**Requirements:** Go 1.22+. Zero external dependencies.

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
| **macOS** | Yes | Yes |
| **Linux** | Yes | Yes |

You can also pass custom paths manually if needed:

```bash
# macOS
./helium-drm-fixer --chrome-path "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/133.0.6943.142/Libraries/WidevineCdm"

# Linux
./helium-drm-fixer --chrome-path "/opt/google/chrome/WidevineCdm"
```

## Troubleshooting

### "Helium browser not found"
Ensure Helium is installed. Download from [https://helium.is/](https://helium.is/). On Linux, launch it at least once before running the tool.

### "Failed to copy WidevineCdm"
Make sure you have write permissions to the Helium application directory. On macOS you may need `sudo`.

### "Failed to re-sign Helium.app" (macOS)
Run with `sudo` or manually execute:
```bash
sudo codesign --force --deep --sign - "/Applications/Helium.app"
```

## Project Structure

```
├── main.go                       # CLI entry point (flag parsing)
├── internal/
│   ├── detector/                 # Chrome & Helium detection (Win, macOS, Linux)
│   │   ├── detector.go           # Path resolution & manifest parser
│   │   ├── registry_windows.go   # Windows registry Chrome detection
│   │   └── registry_other.go     # Non-Windows stub
│   ├── fixer/                    # Orchestration, copy, codesign & hint files
│   │   ├── fixer.go              # Core workflow logic
│   │   ├── logger.go             # Formatted ANSI logger
│   │   └── options.go            # CLI configuration options
│   └── widevine/                 # Widevine CDM acquisition & extraction
│       ├── download.go           # Google Component Updater client & cache
│       └── extract.go            # Pure Go CRX3 / ZIP unpacking
├── go.mod / go.sum               # Go modules
└── LICENSE
```

## Credits

- [Helium Browser](https://helium.is/) — the browser this tool fixes
- Google Chrome Omaha / Component Updater API — official source for Widevine CDM components

## License

MIT
