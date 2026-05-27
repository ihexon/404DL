# mvdl

mvdl is a Go torrent search utility with a local HTTP file browser for selected
search results.

It has two main workflows:

- `server` exposes a small JSON search API.
- `query | httpfs` turns provider search results into a local web UI that can
  load torrent metadata, expose file download URLs, and show live BitTorrent
  runtime diagnostics.

## Search API

Start the API server:

```bash
go run ./cmd/server server
```

Query torrents:

```text
GET /v1/t?search={search term}
```

Example:

```bash
curl --noproxy '*' 'http://127.0.0.1:6567/v1/t?search=mortal%20kombat%20ii%202160p'
```

The response is a normalized JSON array:

```json
[
  {
    "provider": "torrentclaw",
    "title": "Example",
    "bytes": 123456789,
    "seeders": 10,
    "peers": 8,
    "hash": "40b7f6bffcb215e3577ebe55d1090a0c1ec0c64f",
    "magnetUrl": "magnet:?xt=urn:btih:..."
  }
]
```

Errors use a structured response:

```json
{
  "error": {
    "code": "bad_request",
    "message": "search name is required"
  }
}
```

`GET /healthz` returns `{ "status": "ok" }`.

## CLI

Run the API server on the default address `127.0.0.1:6567`:

```bash
go run ./cmd/server server
```

Change the listen address:

```bash
go run ./cmd/server server --listen :18080
```

Search providers directly and print JSON:

```bash
go run ./cmd/server query "mortal kombat ii 2160p"
```

Limit providers while debugging:

```bash
go run ./cmd/server query --provider knaben --provider torrentclaw "mortal kombat ii"
```

Serve query results through the local httpfs UI:

```bash
go run ./cmd/server query "mortal kombat ii" | go run ./cmd/server httpfs --stdin
```

Serve saved query results:

```bash
go run ./cmd/server httpfs --input results.json
```

## httpfs UI

`httpfs` listens on `127.0.0.1:6570` by default. It reads query JSON, opens a
local web UI, and uses anacrolix/torrent to resolve metadata and serve files.

The UI shows:

- Torrent summary, provider, size, seeds, peers, info hash, and magnet link.
- Files from loaded torrent metadata.
- Range-capable download URLs under `/d/{id}/{filePath}`.
- Runtime diagnostics: Peers, DHT, Events, and Pieces.
- A virtualized piece grid where one box is one real BitTorrent piece from the
  torrent-level anacrolix state.

Piece hover text shows the piece index, state, priority, and the latest useful
peer observed for that piece in the current process. A piece can be assembled
from multiple peers, so this is intentionally reported as the latest useful peer
rather than the only source.

Runtime endpoints:

```text
GET /api/torrents
GET /api/torrents/{id}
GET /api/torrents/{id}/files
GET /api/torrents/{id}/runtime
GET /api/torrents/{id}/runtime/stream
```

`/runtime/stream` is an SSE stream. It sends live runtime snapshots so the UI
does not need short-interval polling for torrent diagnostics.

## Configuration

Environment variables:

```text
ADDR=127.0.0.1:6567
PAGE_SIZE=50
UPSTREAM_TIMEOUT=8s
KNABEN_API_URL=https://api.knaben.org/v1
TORRENTCLAW_API_URL=https://torrentclaw.com/api/v1
TORRENTCLAW_API_KEY=
MVDL_CRYKEY=
```

`PAGE_SIZE` is capped at 200 by the API handler.

`TORRENTCLAW_API_KEY` is sent as `Authorization: Bearer <key>` when configured.
TorrentClaw may require an API key for magnet links and torrent download URLs.

`MVDL_CRYKEY` must be exactly 32 bytes. When it is set for `server`, non-empty
`magnetUrl` values are encrypted with AES-256-GCM before being returned. When it
is set for `httpfs`, encrypted magnet values from saved API results are
decrypted before metadata loading.

httpfs flags:

```text
--input          query JSON input file
--stdin          read query JSON from stdin
--listen         HTTP listen address, default 127.0.0.1:6570
--data-dir       torrent data and metadata cache directory
--torrent-listen BitTorrent listen address, default :42069
```

## Docker

Build:

```bash
docker build -t mvdl .
```

Run the API server:

```bash
docker run --rm -p 6567:8080 mvdl
```

Run on a custom address inside the container:

```bash
docker run --rm -p 18080:18080 mvdl server --listen :18080
```

## Notes

Search providers are queried concurrently. If one provider fails, mvdl returns
results from the providers that succeeded. The request fails only when every
configured provider fails or no provider is configured.

Runtime diagnostics are process-local. Peer events, latest useful piece peers,
and DHT observations are collected from the running httpfs process and are not a
historical database.
