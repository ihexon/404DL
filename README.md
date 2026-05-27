# mvdl

Small Go API wrapper around torrent search providers.

## API

```text
GET /v1/t?search={search name}
```

Example:

```bash
curl --noproxy '*' 'http://127.0.0.1:6567/v1/t?search=mortal%20kombat%20ii%202160p'
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

`search` is required. Results are de-duplicated, sorted by seeders descending,
and capped by page size.

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

Search providers directly:

```bash
go run ./cmd/server query 真人快打2
```

Debug one provider at a time:

```bash
go run ./cmd/server query --provider torrentclaw 真人快打2
```

Repeat `--provider` to query a selected set:

```bash
go run ./cmd/server query --provider knaben --provider torrentclaw 真人快打2
```

Serve query results as a local HTTP file index:

```bash
go run ./cmd/server query 真人快打2 | go run ./cmd/server httpfs --stdin
```

`httpfs` starts a React web UI on `127.0.0.1:6570` by default. It loads torrent
metadata on demand, lists torrent files, and exposes Range-capable HTTP download
URLs for each file. Use `--stdin` to read piped query output,
`--input results.json` to read saved query output, `--listen` to change the web
address, and `--data-dir` to choose the torrent cache directory.

Environment variables:

```text
ADDR=127.0.0.1:6567
PAGE_SIZE=200
UPSTREAM_TIMEOUT=8s
KNABEN_API_URL=https://api.knaben.org/v1
TORRENTCLAW_API_URL=https://torrentclaw.com/api/v1
TORRENTCLAW_API_KEY=
MVDL_CRYKEY=
```

`PAGE_SIZE` is capped at 200.

Set `TORRENTCLAW_API_KEY` to send `Authorization: Bearer <key>` to TorrentClaw.
TorrentClaw requires an API key for magnet links and `.torrent` download URLs.

When `MVDL_CRYKEY` is set, every non-empty `magnetUrl` in the JSON response is
encrypted with AES-256-GCM before it is returned. The key must be exactly 32
bytes.

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
