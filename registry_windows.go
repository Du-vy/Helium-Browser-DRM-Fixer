//go:build windows

package main

import (
	"golang.org/x/sys/windows/registry"
)

func registryChromePaths() []string {
	var paths []string

	keys := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`},
	}

	for _, k := range keys {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, err := key.GetStringValue("")
		key.Close()
		if err == nil && val != "" {
			paths = append(paths, val)
		}
	}

	return paths
}
