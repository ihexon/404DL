# mvdl

Small Go API wrapper around torrent search providers.

## API

```text
GET /v1/t?search={search name}&resolution={resolution}
```

Example:

```bash
curl --noproxy '*' 'http://127.0.0.1:6567/v1/t?search=mortal%20kombat%20ii&resolution=2160p'
```

Successful responses return normalized torrent records directly. Raw provider
JSON is not returned.

```json
[
  {
    "provider": "torrentclaw",
    "title": "Example",
    "hash": "example",
    "magnetUrl": "magnet:?xt=urn:btih:...",
    "peers": 10,
    "seeders": 8
  }
]
```

Errors return a structured JSON error:

```json
{
  "error": {
    "code": "bad_request",
    "message": "search name is required"
  }
}
```

`search` and `resolution` are required. The server searches Knaben with only
`search`, searches TorrentClaw with `q` and `quality`, then merges provider
results, filters by resolution, de-duplicates, sorts by seeders descending, and
returns at most 200 records.

Providers are queried concurrently. If one provider fails, results from the
other providers are still returned; the request fails only when every provider
fails or no providers are configured.

## Run

```bash
go run ./cmd/server
```

Configure the listen address:

```bash
go run ./cmd/server --listen :18080
```

Help:

```bash
go run ./cmd/server --help
```

Search providers directly:

```bash
go run ./cmd/server query --resolution 1080p 真人快打2
```

Generate an AES-256 key for `MVDL_CRYKEY`:

```bash
go run ./cmd/server gen-key
```

Download a magnet URL:

```bash
go run ./cmd/server download --save-to ./downloads 'magnet:?xt=urn:btih:...'
```

Download a `.torrent` file:

```bash
go run ./cmd/server download --save-to ./downloads ./movie.torrent
```

Download an encrypted `magnetUrl` returned by the API:

```bash
MVDL_CRYKEY=your-32-byte-key go run ./cmd/server download --save-to ./downloads 'encrypted-magnet-url'
```

`download` treats existing local files ending in `.torrent` as torrent files,
values starting with `magnet:` as plain magnet URLs, and other values as
encrypted magnet URLs decrypted with `MVDL_CRYKEY`.
Download progress reports total size, downloaded size, and current download
speed every second by default. Use `--progress-interval N` to change the
interval.

Expose the full anacrolix/torrent status output over HTTP:

```bash
go run ./cmd/server download --save-to ./downloads --status-listen 127.0.0.1:6570 ./movie.torrent
curl http://127.0.0.1:6570/status
```

The downloader uses an aggressive single-download profile by default:

```text
--progress-interval 1
--connections 160
--half-open 80
--total-half-open 240
--peer-high-water 2000
--peer-low-water 200
--dial-rate 80
--max-unverified-mib 512
--peer-request-buffer-mib 4
--piece-hashers 4
```

These tune anacrolix/torrent peer discovery, peer dialing, concurrent
connections, piece verification backlog, and hash workers. Leave upload enabled
for best swarm reciprocity; use `--upload-rate-mib` to cap upload if needed.
`--no-upload` is available, but can reduce download speed in many swarms.

Environment variables:

```text
ADDR=127.0.0.1:6567
PAGE_SIZE=200
UPSTREAM_TIMEOUT=8s
KNABEN_API_URL=https://api.knaben.org/v1
TORRENTCLAW_API_URL=https://torrentclaw.com/api/v1
MVDL_TMDB_APIKEY=
TMDB_API_URL=https://api.themoviedb.org/3
MVDL_CRYKEY=
```

`PAGE_SIZE` is capped at 200.

When `MVDL_CRYKEY` is set, every non-empty `magnetUrl` in the JSON response is
encrypted with AES-256-GCM before it is returned. The key must be exactly 32
bytes.

When `MVDL_TMDB_APIKEY` is set, search terms are first resolved with TMDb movie
search. The resolved English/original title plus release year is sent to the
torrent providers. `MVDL_TMDB_APIKEY` may be either a TMDb v3 API key or a TMDb
API Read Access Token.

If TMDb returns an error, for example because the key expired, the server logs a
warning to stdout/stderr and falls back to the original search term.

## Docker

Build:

```bash
docker build -t mvdl .
```

Run:

```bash
docker run --rm -p 6567:8080 mvdl
```

Configure the listen address inside the container:

```bash
docker run --rm -p 18080:18080 mvdl --listen :18080
```
