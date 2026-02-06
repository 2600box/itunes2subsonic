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
* `--sync_starred` (default: `true`): Sync loved tracks to Navidrome starred.
* `--sync_playlists` (default: `true`): Sync playlists.
* `--max_scrobbles` (default: `250`): Max scrobbles per track when syncing play counts.
* `--itunes_root` (optional): Music library prefix for Apple Music paths.
* `--subsonic_root` (optional): Music library prefix for Navidrome paths.
* `--filter_album`: Only sync tracks whose album contains this text (comma-separated).
* `--filter_artist`: Only sync tracks whose artist contains this text (comma-separated).
* `--filter_name`: Only sync tracks whose title contains this text (comma-separated).
* `--filter_path`: Only sync tracks whose path contains this text (comma-separated).
* `--limit_tracks` (default: `0`): Only sync the first N matching tracks (0 means no limit).
* `--debug` (default: `false`): Enable debug logging for filters/matching.
* `--log_file` (default: empty): Write logs to the specified file (logs still go to stderr).
* `--write_missing` (default: empty): Write a JSON report of missing tracks to the given path.
* `--created_file` (unused): Placeholder for writing SQL to update created timestamps.

When filters are used, automatic library root detection is skipped; provide `--itunes_root` and
`--subsonic_root` if the library paths differ between Apple Music and Navidrome.

Path matching normalizes Unicode to NFC to handle macOS (NFD) vs Linux (NFC) filesystem differences.

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
