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

Open the interactive OpenAPI documentation:

```text
GET /docs
GET /openapi.json
```

Search files:

```text
GET /v1/search?q={search query}&limit={max results}&provider={provider}
```

Example:

```bash
curl --noproxy '*' 'http://127.0.0.1:6567/v1/search?q=mortal%20kombat%20ii%202160p&limit=3'
```

Repeat `provider` to select multiple providers. If `provider` is omitted, the
server searches every configured provider. The OpenAPI document defines
parameters, response schemas, and error schemas.

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

Search files and print JSON:

```bash
go run ./cmd/server search "mortal kombat ii 2160p"
```

By default, `search` starts an embedded Search API server for the current
command and queries all providers.

Use a non-default API server:

```bash
go run ./cmd/server search --server-url http://127.0.0.1:18080 "mortal kombat ii"
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

Non-sensitive settings are configured with flags:

```text
server --listen 127.0.0.1:6567 --limit-size 50 --timeout 8s
search --limit-size 50 --timeout 8s
```

The server's default limit is used when the API `limit` parameter is omitted.
API limits are capped at 200 by the API handler.

Environment variables are reserved for sensitive values:

```text
TORRENTCLAW_API_KEY=
MVDL_CRYKEY=
```

`TORRENTCLAW_API_KEY` is sent as `Authorization: Bearer <key>` when configured.
TorrentClaw may require an API key for magnet links.

`MVDL_CRYKEY` must be exactly 32 bytes. When it is set for `server`, non-empty
`magnetUrl` values are encrypted with AES-256-GCM before being returned. When it
is set for `get`, encrypted magnet values from saved API results are decrypted
before metadata loading.

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
