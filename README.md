# apple-music2navidrome

Personal project for copying ratings between music instances.

Hiring folks, please don't judge me on this code. 😛

## Apple Music -> Navidrome

Copies ratings, play counts, last played dates, loved (starred) tracks, and playlists set in Apple Music to Navidrome (via its Subsonic API). Safe to run on an ongoing basis (although it cannot sync back to Apple Music).

```sh
$ export SUBSONIC_USER=my_user
$ export SUBSONIC_PASS="my navidrome password"
$ go run github.com/logank/itunes2subsonic/cmd/itunes2subsonic --itunes_xml="Apple Music Library.xml" --subsonic="https://navidrome.example.com" --dry_run=false
```

Use filters to test on a subset of tracks:

```sh
$ go run github.com/logank/itunes2subsonic/cmd/itunes2subsonic --itunes_xml="Apple Music Library.xml" --subsonic="https://navidrome.example.com" --filter_album="OK Computer" --filter_artist="Radiohead" --limit_tracks=25 --dry_run=false
```

### itunes2subsonic options

* `--dry_run` (default: `true`): Don't modify the destination library.
* `--itunes_xml` (default: `Apple Music Library.xml`): Path to the Apple Music Library XML file.
* `--subsonic` (required unless saved in config): Navidrome base URL.
* `--skip_count` (default: `10`): Maximum number of errors to tolerate before stopping.
* `--copy_unrated` (default: `false`): Clear Navidrome ratings if Apple Music is unrated.
* `--update_played` (default: `true`): Sync play count and last played date.
* `--sync_starred` (default: `true`): Sync favorited/loved tracks to Navidrome starred.
* `--sync_playlists` (default: `true`): Sync playlists.
* `--max_scrobbles` (default: `250`): Max scrobbles per track when syncing play counts.
* `--itunes_root` (optional): Music library prefix for Apple Music paths.
* `--subsonic_root` (optional): Music library prefix for Navidrome paths.
* `--extensions` (default: common audio types): Comma-separated allowlist of extensions (excludes `.mp4` unless added).
* `--verify_src_files` (default: `false`): Verify iTunes file paths exist on disk and classify stale entries in the missing report.
* `--filter_album`: Only sync tracks whose album contains this text (comma-separated).
* `--filter_artist`: Only sync tracks whose artist contains this text (comma-separated).
* `--filter_name`: Only sync tracks whose title contains this text (comma-separated).
* `--filter_path`: Only sync tracks whose path contains this text (comma-separated).
* `--limit_tracks` (default: `0`): Only sync the first N matching tracks (0 means no limit).
* `--debug` (default: `false`): Enable debug logging for filters/matching.
* `--log_file` (default: empty): Write logs to the specified file (logs still go to stderr).
* `--navidrome_dump` (default: empty): Write a JSON dump of Navidrome track metadata.
* `--write_missing` (default: empty): Write a JSON report of missing tracks to the given path.
* `--analyse_dump` (default: empty): Analyse a Navidrome dump JSON file and print a summary.
* `--analyse_report` (default: empty): Write dump analysis output to JSON (used with `--analyse_dump`).
* `--analyse_missing` (default: empty): Include an existing missing report in dump analysis.
* `--created_file` (unused): Placeholder for writing SQL to update created timestamps.

When filters are used, automatic library root detection is skipped; provide `--itunes_root` and
`--subsonic_root` if the library paths differ between Apple Music and Navidrome.

Path matching normalizes Unicode to NFC to handle macOS (NFD) vs Linux (NFC) filesystem differences.

### Loved/Favorited → Starred

When syncing stars, Apple Music `Favorited` is preferred when present. If `Favorited` is absent, the legacy `Loved` flag is used. The sync output will label this as “Favourited/Loved → Starred.”

### Missing reports

Use `--write_missing` to generate a JSON report with counts, missing entries, and classifications. Missing entries only include actionable mismatches between eligible Apple Music file-backed tracks and eligible Navidrome tracks. Apple Music streaming catalog entries (Track Type `Remote` / `Apple Music` true / missing `Location`) are excluded from matching, but are counted and sampled separately. The default extension allowlist excludes video formats like `.mp4` unless added via `--extensions`.

Default extensions: `.mp3`, `.m4a`, `.flac`, `.ogg`, `.opus`, `.aac`, `.wav`, `.aiff`, `.alac`.

### Analyse a dump locally

To analyse a large Navidrome dump file locally without uploading it:

```sh
$ go run github.com/logank/itunes2subsonic/cmd/itunes2subsonic --analyse_dump dump.json
```

Include a missing report for reason summaries:

```sh
$ go run github.com/logank/itunes2subsonic/cmd/itunes2subsonic --analyse_dump dump.json --analyse_missing missing.json --analyse_report analysis.json
```

## Navidrome -> Navidrome

Copies ratings set in a Navidrome server to a different Navidrome server. Safe to run on an ongoing basis, but there is insufficient data to identify "newer" ratings so best used to sync in one direction. 

```sh
$ export SUBSONIC_USER=navidrome_user
$ export SUBSONIC_PASS="my navidrome password"
$ export SUBSONIC_USER=ampache_user
$ export SUBSONIC_PASS="my ampache password"
$ go run github.com/logank/itunes2subsonic/cmd/subsonic2subsonic --subsonic_src="https://navidrome.example.com" --subsonic_dst="https://ampache.example.com" --dry_run=false
```

### subsonic2subsonic options

* `--dry_run` (default: `true`): Don't modify the destination library.
* `--skip_count` (default: `10`): Maximum number of errors to tolerate before stopping.
* `--copy_unrated` (default: `false`): Clear destination ratings if source is unrated.
* `--subsonic_src` (required): Source Navidrome base URL.
* `--subsonic_dst` (required): Destination Navidrome base URL.
* `--subsonic_src_root` (optional): Music library prefix for source paths.
* `--subsonic_dst_root` (optional): Music library prefix for destination paths.

## Apple Music -> Ampache

Copies ratings set in Apple Music to an Ampache server. Safe to run on an ongoing basis (although it cannot sync back to Apple Music).

> **Note**
> Use the Navidrome sync instead; the Ampache API does not currently provide more advanced support.
