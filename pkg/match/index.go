package match

import (
	"sort"
	"strings"
)

type Candidate struct {
	SongID          string `json:"song_id"`
	Path            string `json:"path,omitempty"`
	Artist          string `json:"artist,omitempty"`
	Album           string `json:"album,omitempty"`
	Title           string `json:"title,omitempty"`
	NormArtist      string `json:"norm_artist,omitempty"`
	NormAlbum       string `json:"norm_album,omitempty"`
	NormTitle       string `json:"norm_title,omitempty"`
	NormArtistToken []string
	NormAlbumToken  []string
	NormTitleToken  []string
	Score           float64 `json:"score"`
	Method          string  `json:"match_method,omitempty"`
}

type IndexEntry struct {
	SongID     string
	Path       string
	Artist     string
	Album      string
	Title      string
	NormArtist string
	NormAlbum  string
	NormTitle  string
	ArtistTok  []string
	AlbumTok   []string
	TitleTok   []string
}

type Index struct {
	entries         []IndexEntry
	artistBuckets   map[string][]int
	titleBuckets    map[string][]int
	combinedBuckets map[string][]int
}

func BuildIndex(entries []IndexEntry) *Index {
	idx := &Index{
		entries:         entries,
		artistBuckets:   make(map[string][]int),
		titleBuckets:    make(map[string][]int),
		combinedBuckets: make(map[string][]int),
	}
	for i, entry := range entries {
		artistKey := artistBucketKey(entry.NormArtist)
		titleKey := titleBucketKey(entry.NormTitle)
		if artistKey != "" {
			idx.artistBuckets[artistKey] = append(idx.artistBuckets[artistKey], i)
		}
		if titleKey != "" {
			idx.titleBuckets[titleKey] = append(idx.titleBuckets[titleKey], i)
		}
		if artistKey != "" && titleKey != "" {
			combined := artistKey + "|" + titleKey
			idx.combinedBuckets[combined] = append(idx.combinedBuckets[combined], i)
		}
	}
	return idx
}

func (idx *Index) Candidates(normArtist string, normTitle string) []IndexEntry {
	artistKey := artistBucketKey(normArtist)
	titleKey := titleBucketKey(normTitle)
	combinedKey := ""
	if artistKey != "" && titleKey != "" {
		combinedKey = artistKey + "|" + titleKey
	}
	seen := make(map[int]struct{})
	candidates := make([]IndexEntry, 0)
	appendEntry := func(index int) {
		if _, ok := seen[index]; ok {
			return
		}
		seen[index] = struct{}{}
		candidates = append(candidates, idx.entries[index])
	}
	if combinedKey != "" {
		for _, index := range idx.combinedBuckets[combinedKey] {
			appendEntry(index)
		}
	}
	if len(candidates) == 0 && artistKey != "" {
		for _, index := range idx.artistBuckets[artistKey] {
			appendEntry(index)
		}
	}
	if len(candidates) == 0 && titleKey != "" {
		for _, index := range idx.titleBuckets[titleKey] {
			appendEntry(index)
		}
	}
	return candidates
}

func artistBucketKey(normArtist string) string {
	if normArtist == "" {
		return ""
	}
	key := strings.ReplaceAll(normArtist, " ", "")
	if len(key) > 12 {
		key = key[:12]
	}
	return key
}

func titleBucketKey(normTitle string) string {
	if normTitle == "" {
		return ""
	}
	parts := strings.Fields(normTitle)
	if len(parts) == 0 {
		return ""
	}
	key := parts[0]
	if len(key) > 12 {
		key = key[:12]
	}
	return key
}

func SortEntries(entries []IndexEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Artist != entries[j].Artist {
			return entries[i].Artist < entries[j].Artist
		}
		if entries[i].Album != entries[j].Album {
			return entries[i].Album < entries[j].Album
		}
		if entries[i].Title != entries[j].Title {
			return entries[i].Title < entries[j].Title
		}
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].SongID < entries[j].SongID
	})
}
