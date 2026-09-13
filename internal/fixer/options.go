package fixer

// Options defines the command line flags and operational modes for fixing DRM.
type Options struct {
	Verbose       bool
	DryRun        bool
	Check         bool
	ChromePath    string
	HeliumPath    string
	ForceDownload bool
}
