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

The response is a merged JSON array from all configured providers.

`search` and `resolution` are required. The server searches Knaben with only
`search`, searches TorrentClaw with `q` and `quality`, then merges provider
results, filters by resolution, de-duplicates, sorts by seeders descending, and
returns at most 200 records.

Providers are queried concurrently. If one provider fails, results from the
other providers are still returned; the request fails only when every provider
fails or no providers are configured.

## Run

```bash
go run ./cmd/server server
```

Configure the listen address:

```bash
go run ./cmd/server server --listen :18080
```

Help:

```bash
go run ./cmd/server --help
```

Manual search through a running server:

```bash
go run ./cmd/server query 真人快打2 --resolution 1080p
```

Use a non-default server address:

```bash
go run ./cmd/server query 真人快打2 --addr http://127.0.0.1:18080 --resolution 1080p
```

By default, `query` connects to:

```text
http://127.0.0.1:6567
```

Generate an AES-256 key for `MVDL_CRYKEY`:

```bash
go run ./cmd/server gen-key
```

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
docker run --rm -p 18080:18080 mvdl server --listen :18080
```
