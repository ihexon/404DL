# mvdl

mvdl is a Go file search and download utility with a local web UI for selected
search results.

It has two main workflows:

- `server` exposes a small JSON file search API.
- `search | get` turns provider search results into a local web UI that can
  load torrent metadata, download files through BitTorrent, and show live
  runtime diagnostics.

## Search API

Start the API server:

```bash
go run ./cmd/server server
```

Search files:

```text
GET /v1/search?q={search query}
```

Example:

```bash
curl --noproxy '*' 'http://127.0.0.1:6567/v1/search?q=mortal%20kombat%20ii%202160p'
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
    "message": "search query is required"
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

Search files through providers and print JSON:

```bash
go run ./cmd/server search "mortal kombat ii 2160p"
```

Limit providers while debugging:

```bash
go run ./cmd/server search --provider knaben --provider torrentclaw "mortal kombat ii"
```

Serve search results through the local get UI:

```bash
go run ./cmd/server search "mortal kombat ii" | go run ./cmd/server get --stdin
```

Serve saved search results:

```bash
go run ./cmd/server get --input results.json
```

## get UI

`get` listens on `127.0.0.1:6570` by default. It reads search result JSON, opens a
local web UI, and uses anacrolix/torrent to resolve metadata and download files
directly through BitTorrent into `--save-to`.

The UI shows:

- Torrent summary, provider, size, seeds, peers, info hash, and magnet link.
- Torrent-level `Start`, `Pause`, and `Delete` actions.
- Files from loaded torrent metadata as a read-only view of torrent contents.
- Runtime diagnostics: Peers, DHT, Events, and Pieces.
- A virtualized piece grid where one box is one real BitTorrent piece from the
  torrent-level anacrolix state.

Piece hover text shows the piece index, state, and priority reported by
anacrolix for diagnostics. The UI colors pieces by actual torrent state.

get API:

```text
GET /api/torrents
GET /api/torrents/{id}
GET /api/torrents/{id}/stream
POST /api/torrents/{id}/start
POST /api/torrents/{id}/pause
POST /api/torrents/{id}/delete
```

`/api/torrents` is a cheap list endpoint and does not add torrents to the
BitTorrent runtime. Torrent download actions load metadata when needed through
anacrolix/torrent. The per-torrent SSE stream pushes live updates only for
active torrents.

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
TorrentClaw may require an API key for magnet links.

`MVDL_CRYKEY` must be exactly 32 bytes. When it is set for `server` or `search`,
non-empty `magnetUrl` values are encrypted with AES-256-GCM before being
returned. When it is set for `get`, encrypted magnet values from saved API
results are decrypted before metadata loading.

get flags:

```text
--input          search result JSON input file
--stdin          read search result JSON from stdin
--listen         HTTP listen address, default 127.0.0.1:6570
--save-to        directory to save downloaded files
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
and DHT observations are collected from the running get process and are not a
historical database.
