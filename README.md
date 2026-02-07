# apple-music2navidrome

Personal project for copying ratings between music instances.

## Apple Music -> Navidrome

Copies ratings, play counts, last played dates, loved (starred) tracks, and playlists set in Apple Music to Navidrome (via its Subsonic API). Safe to run on an ongoing basis (although it cannot sync back to Apple Music).

```sh
$ export SUBSONIC_USER=my_user
$ export SUBSONIC_PASS="my navidrome password"
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
* `--report_library_stats` (default: empty): Write Library.xml stats report to JSON.
* `--report_sync_plan` (default: empty): Write a detailed sync plan to JSON.
* `--report_sync_plan_tsv` (default: empty): Write sync plan TSV reports using the given path as a base name.
* `--report_reconcile` (default: empty): Write a reconcile report comparing Library.xml stats and sync plan counts (requires `--report_sync_plan`).
* `--out_tsv` (default: empty): Write a TSV summary when reporting library stats.
* `--report_only` (default: false): Avoid fetching the full Navidrome song list when filters are active (requires `--navidrome_dump`).
* `--allow_unstar` (default: false): Allow unstar operations when `--dry_run=false`.
* `--allow_reconcile_mismatch` (default: false): Allow reconcile invariant mismatches (writes report, exits 0).
* `--created_file` (unused): Placeholder for writing SQL to update created timestamps.

When filters are used, automatic library root detection is skipped; provide `--itunes_root` and
`--subsonic_root` if the library paths differ between Apple Music and Navidrome.

Path matching normalizes Unicode to NFC to handle macOS (NFD) vs Linux (NFC) filesystem differences.

### Loved/Favorited → Starred

When syncing stars, Apple Music `Favorited` is preferred when present. If `Favorited` is absent, the legacy `Loved` flag is used. The sync output will label this as “Favourited/Loved → Starred.”

### Auditing & reconciliation reports

Use the reporting flags to produce deterministic audit artifacts before running with `--dry_run=false`:

* `--report_sync_plan PATH` writes `sync_plan.json`, including `schema_version`, `generated_at`, Navidrome baseline counts, and the full mutation plan.
* `--report_reconcile PATH` writes `reconcile.json`, which contains:
  * Apple Library.xml disaggregation (tracks/loved/rated/loved-only/rated-only split by local/remote).
  * Navidrome baseline totals (tracks/starred/rated).
  * Plan counts for stars, unstars, rating sets/unsets, playcount updates, and playlist ops.
  * Loved→Starred reconciliation fields plus the invariant check (fails the run unless `--allow_reconcile_mismatch=true`).
* The run directory (same as `--log_file`, or next to the report path) also receives TSVs:
  * `plan_star.tsv`, `plan_unstar.tsv`, `unapplied_loved.tsv`, and `unapplied_rated.tsv` (if rating mismatches are reconciled).

Each TSV uses the columns:

```
op    navidrome_id    apple_track_id    artist    album    title    path    reason_code    match_mode    match_confidence
```

These artifacts are designed to be diffed between runs and to make it explicit which tracks will be starred/unstarred and why.

### Missing reports

Use `--write_missing` to generate a JSON report with counts, missing entries, and classifications. Missing entries only include actionable mismatches between eligible Apple Music file-backed tracks and eligible Navidrome tracks. Apple Music streaming catalog entries (Track Type `Remote` / `Apple Music` true / missing `Location`) are excluded from matching, but are counted and sampled separately. The default extension allowlist excludes video formats like `.mp4` unless added via `--extensions`.

Default extensions: `.mp3`, `.m4a`, `.flac`, `.ogg`, `.opus`, `.aac`, `.wav`, `.aiff`, `.alac`.

### Analyse a dump locally

To analyse a large Navidrome dump file locally without uploading it:

```sh
$ go run github.com/2600box/itunes2subsonic/cmd/itunes2subsonic --analyse_dump dump.json
```

Include a missing report for reason summaries:

```sh
$ go run github.com/2600box/itunes2subsonic/cmd/itunes2subsonic --analyse_dump dump.json --analyse_missing missing.json --analyse_report analysis.
