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
		segmentMatcher := newSegmentMatcher(segment)
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", false
		}
		match := ""
		matches := 0
		for _, entry := range entries {
			name := entry.Name()
			if segmentMatcher.matches(name) {
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

type segmentMatcher struct {
	raw string
	nfc string
	nfd string
}

func newSegmentMatcher(segment string) segmentMatcher {
	return segmentMatcher{
		raw: segment,
		nfc: norm.NFC.String(segment),
		nfd: norm.NFD.String(segment),
	}
}

func (m segmentMatcher) matches(candidate string) bool {
	if m.raw == candidate {
		return true
	}
	if strings.EqualFold(m.raw, candidate) {
		return true
	}
	candidateNFC := norm.NFC.String(candidate)
	if strings.EqualFold(m.nfc, candidateNFC) {
		return true
	}
	candidateNFD := norm.NFD.String(candidate)
	return strings.EqualFold(m.nfd, candidateNFD)
}
