package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func resolvePathOnDisk(pathValue string) (string, bool) {
	if pathValue == "" {
		return "", false
	}
	cleaned := filepath.Clean(pathValue)
	if cleaned == "." {
		return "", false
	}
	volume := filepath.VolumeName(cleaned)
	rest := strings.TrimPrefix(cleaned, volume)
	isAbs := filepath.IsAbs(cleaned)
	if isAbs {
		rest = strings.TrimLeft(rest, string(os.PathSeparator))
	}
	segments := strings.Split(rest, string(os.PathSeparator))
	current := volume
	if isAbs {
		if volume == "" {
			current = string(os.PathSeparator)
		} else {
			current = volume + string(os.PathSeparator)
		}
	} else if current == "" {
		current = "."
	}

	for _, segment := range segments {
		if segment == "" {
			continue
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", false
		}
		match := ""
		matches := 0
		for _, entry := range entries {
			name := entry.Name()
			if segmentMatches(segment, name) {
				matches++
				match = name
				if matches > 1 {
					break
				}
			}
		}
		if matches != 1 {
			return "", false
		}
		current = filepath.Join(current, match)
	}
	if _, err := os.Stat(current); err != nil {
		return "", false
	}
	return current, true
}

func segmentMatches(a, b string) bool {
	if a == b {
		return true
	}
	if strings.EqualFold(a, b) {
		return true
	}
	if strings.EqualFold(norm.NFC.String(a), norm.NFC.String(b)) {
		return true
	}
	if strings.EqualFold(norm.NFD.String(a), norm.NFD.String(b)) {
		return true
	}
	return false
}
