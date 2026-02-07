package report

import (
	"fmt"
	"sort"
	"time"

	"github.com/logank/itunes2subsonic/internal/report"
	"github.com/logank/itunes2subsonic/pkg/match"
)

type RemoteTrackInput struct {
	AppleTrackID      int
	ApplePersistentID string
	Loved             bool
	Rating            int
	Artist            string
	Album             string
	Title             string
}

type NavidromeInput struct {
	SongID string
	Path   string
	Artist string
	Album  string
	Title  string
}

type RemoteMatchConfig struct {
	TopN         int
	Threshold    float64
	LowThreshold float64
}

type RemoteMatchResult struct {
	Report report.RemoteMatchReport
}

func BuildRemoteMatchReport(version string, remoteTracks []RemoteTrackInput, navidrome []NavidromeInput, cfg RemoteMatchConfig) RemoteMatchResult {
	if cfg.TopN <= 0 {
		cfg.TopN = 5
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.87
	}
	if cfg.LowThreshold <= 0 {
		cfg.LowThreshold = 0.75
	}

	indexEntries := make([]match.IndexEntry, 0, len(navidrome))
	for _, song := range navidrome {
		normArtist := match.NormalizeText(song.Artist)
		normAlbum := match.NormalizeText(song.Album)
		normTitle := match.NormalizeText(song.Title)
		indexEntries = append(indexEntries, match.IndexEntry{
			SongID:     song.SongID,
			Path:       song.Path,
			Artist:     song.Artist,
			Album:      song.Album,
			Title:      song.Title,
			NormArtist: normArtist,
			NormAlbum:  normAlbum,
			NormTitle:  normTitle,
			ArtistTok:  match.Tokens(normArtist),
			AlbumTok:   match.Tokens(normAlbum),
			TitleTok:   match.Tokens(normTitle),
		})
	}
	match.SortEntries(indexEntries)
	index := match.BuildIndex(indexEntries)

	remoteEntries := make([]RemoteTrackInput, len(remoteTracks))
	copy(remoteEntries, remoteTracks)
	sort.Slice(remoteEntries, func(i, j int) bool {
		a := remoteEntries[i]
		b := remoteEntries[j]
		if a.Artist != b.Artist {
			return a.Artist < b.Artist
		}
		if a.Album != b.Album {
			return a.Album < b.Album
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.AppleTrackID < b.AppleTrackID
	})

	entries := make([]report.RemoteMatchEntry, 0, len(remoteEntries))
	statusCounts := map[string]int{
		string(report.RemoteMatchStatusMatch):         0,
		string(report.RemoteMatchStatusLowConfidence): 0,
		string(report.RemoteMatchStatusNoMatch):       0,
	}
	remoteLoved := 0
	remoteRated := 0
	remoteLovedAndRated := 0
	lowConfidence := make([]report.RemoteMatchEntry, 0)

	for _, track := range remoteEntries {
		if track.Loved {
			remoteLoved++
		}
		if track.Rating > 0 {
			remoteRated++
		}
		if track.Loved && track.Rating > 0 {
			remoteLovedAndRated++
		}
		normArtist := match.NormalizeText(track.Artist)
		normAlbum := match.NormalizeText(track.Album)
		normTitle := match.NormalizeText(track.Title)
		artistTokens := match.Tokens(normArtist)
		albumTokens := match.Tokens(normAlbum)
		titleTokens := match.Tokens(normTitle)

		candidates := index.Candidates(normArtist, normTitle)
		scored := make([]match.Candidate, 0, len(candidates))
		for _, candidate := range candidates {
			artistExact := candidate.NormArtist == normArtist && normArtist != ""
			albumExact := candidate.NormAlbum == normAlbum && normAlbum != ""
			titleExact := candidate.NormTitle == normTitle && normTitle != ""
			score, method := match.ScoreComposite(artistTokens, albumTokens, titleTokens, candidate.ArtistTok, candidate.AlbumTok, candidate.TitleTok, artistExact, albumExact, titleExact)
			scored = append(scored, match.Candidate{
				SongID:          candidate.SongID,
				Path:            candidate.Path,
				Artist:          candidate.Artist,
				Album:           candidate.Album,
				Title:           candidate.Title,
				NormArtist:      candidate.NormArtist,
				NormAlbum:       candidate.NormAlbum,
				NormTitle:       candidate.NormTitle,
				NormArtistToken: candidate.ArtistTok,
				NormAlbumToken:  candidate.AlbumTok,
				NormTitleToken:  candidate.TitleTok,
				Score:           score,
				Method:          method,
			})
		}
		scored = match.StableSortCandidates(scored, cfg.TopN)
		best := match.Candidate{}
		if len(scored) > 0 {
			best = scored[0]
		}
		status := report.RemoteMatchStatusNoMatch
		if len(scored) > 0 {
			if best.Score >= cfg.Threshold {
				status = report.RemoteMatchStatusMatch
			} else if best.Score >= cfg.LowThreshold {
				status = report.RemoteMatchStatusLowConfidence
			}
		}
		statusCounts[string(status)]++

		entry := report.RemoteMatchEntry{
			AppleTrackID:      track.AppleTrackID,
			ApplePersistentID: track.ApplePersistentID,
			Loved:             track.Loved,
			Rating:            track.Rating,
			Artist:            track.Artist,
			Album:             track.Album,
			Title:             track.Title,
			NormalizedArtist:  normArtist,
			NormalizedAlbum:   normAlbum,
			NormalizedTitle:   normTitle,
			MatchStatus:       status,
			MatchScore:        best.Score,
			MatchMethod:       best.Method,
			CandidateCount:    len(candidates),
		}
		if status != report.RemoteMatchStatusNoMatch && len(scored) > 0 {
			entry.MatchedSongID = best.SongID
			entry.MatchedPath = best.Path
		}
		if len(scored) > 0 {
			entry.TopCandidates = make([]report.RemoteMatchCandidate, 0, len(scored))
			for _, candidate := range scored {
				entry.TopCandidates = append(entry.TopCandidates, report.RemoteMatchCandidate{
					SongID:      candidate.SongID,
					Path:        candidate.Path,
					Artist:      candidate.Artist,
					Album:       candidate.Album,
					Title:       candidate.Title,
					Score:       candidate.Score,
					MatchMethod: candidate.Method,
				})
			}
		}
		if status == report.RemoteMatchStatusLowConfidence {
			lowConfidence = append(lowConfidence, entry)
		}
		entries = append(entries, entry)
	}

	sort.Slice(lowConfidence, func(i, j int) bool {
		if lowConfidence[i].MatchScore != lowConfidence[j].MatchScore {
			return lowConfidence[i].MatchScore > lowConfidence[j].MatchScore
		}
		if lowConfidence[i].Artist != lowConfidence[j].Artist {
			return lowConfidence[i].Artist < lowConfidence[j].Artist
		}
		if lowConfidence[i].Title != lowConfidence[j].Title {
			return lowConfidence[i].Title < lowConfidence[j].Title
		}
		return lowConfidence[i].AppleTrackID < lowConfidence[j].AppleTrackID
	})
	if len(lowConfidence) > 20 {
		lowConfidence = lowConfidence[:20]
	}

	summary := report.RemoteMatchSummary{
		RemoteLovedTotal:         remoteLoved,
		RemoteRatedTotal:         remoteRated,
		RemoteLovedAndRatedTotal: remoteLovedAndRated,
		MatchStatusCounts:        statusCounts,
		LowConfidenceTop:         lowConfidence,
	}

	result := report.RemoteMatchReport{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Version:       version,
		TopN:          cfg.TopN,
		Threshold:     cfg.Threshold,
		LowThreshold:  cfg.LowThreshold,
		Summary:       summary,
		Entries:       entries,
	}

	return RemoteMatchResult{Report: result}
}

func TSVHeaderRemoteMatch() []string {
	return []string{
		"apple_track_id",
		"apple_persistent_id",
		"loved",
		"rating",
		"artist",
		"album",
		"title",
		"normalized_artist",
		"normalized_album",
		"normalized_title",
		"match_status",
		"matched_navidrome_song_id",
		"matched_path",
		"match_score",
		"match_method",
		"candidate_count",
	}
}

func TSVRowsRemoteMatch(entries []report.RemoteMatchEntry) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rating := ""
		if entry.Rating > 0 {
			rating = fmt.Sprintf("%d", entry.Rating)
		}
		matchScore := ""
		if entry.MatchScore > 0 {
			matchScore = fmt.Sprintf("%.4f", entry.MatchScore)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", entry.AppleTrackID),
			entry.ApplePersistentID,
			fmt.Sprintf("%t", entry.Loved),
			rating,
			entry.Artist,
			entry.Album,
			entry.Title,
			entry.NormalizedArtist,
			entry.NormalizedAlbum,
			entry.NormalizedTitle,
			string(entry.MatchStatus),
			entry.MatchedSongID,
			entry.MatchedPath,
			matchScore,
			entry.MatchMethod,
			fmt.Sprintf("%d", entry.CandidateCount),
		})
	}
	return rows
}
