# 404 Downloader

404 Downloader is a local torrent search and download web app.

It starts one local HTTP server with a browser UI where you can search supported
providers, choose a result to download, pause or delete downloads, and inspect
runtime progress.

## What It Does

- Searches torrent providers from the web UI.
- Shows search results as candidates; they are not added to BitTorrent until selected.
- Downloads through BitTorrent into an explicit save directory.
- Shows files, peers, pieces, DHT state, and recent runtime events.
- Exposes a local OpenAPI-documented HTTP API for automation.

## Quick Start

Build the app:

```bash
make build
```

The binary is written to `./bin/4dl`.

Start the web UI:

```bash
./bin/4dl --save-to ~/Downloads
```

Use a fixed listen address when needed:

```bash
./bin/4dl --listen 127.0.0.1:6570 --save-to ~/Downloads
```

Open the logged web URL in your browser. Search for a torrent, then click
Download on one result. New searches replace only the search results; active,
paused, and completed downloads remain.

## HTTP API

The web app also exposes local API docs:

```text
http://127.0.0.1:6570/api/docs/
```

Useful endpoints:

```text
GET  /api/healthz
POST /api/search
GET  /api/torrents
POST /api/torrents
GET  /api/torrents/stream
GET  /api/torrents/stream2
POST /api/torrents/{id}/start
POST /api/torrents/{id}/pause
POST /api/torrents/{id}/delete
```

`/api/torrents/stream` is Server-Sent Events. Swagger UI may not be reliable
for trying that endpoint directly; use `curl -N` or browser `EventSource`.

## Configuration

Common options:

```text
--listen 127.0.0.1:6570
--save-to ~/Downloads
--torrent-listen :42069
--limit-size 50
--timeout 8s
```

Environment variables:

```text
TORRENTCLAW_API_KEY=
```

`TORRENTCLAW_API_KEY` is used when TorrentClaw requires an API key.

## Docker

Build:

```bash
docker build -t 404-downloader .
```

Run:

```bash
docker run --rm -p 8080:8080 -v "$HOME/Downloads:/app/downloads" 404-downloader
```

Open:

```text
http://127.0.0.1:8080
```
