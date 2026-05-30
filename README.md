# 404 Downloader

404 Downloader is a local torrent discovery and download workspace. It gives you
a browser-based control panel for searching supported torrent indexes, reviewing
candidates, and managing downloads from one place.

The app runs on your machine, stores files in a default download directory, and
keeps search results separate from active downloads until you explicitly start
one.

## Why Use It

- Search supported torrent providers without leaving the local web UI.
- Review result details before adding anything to the download queue.
- Start, pause, resume, and delete downloads from a single dashboard.
- Track progress with file, peer, piece, DHT, and runtime event views.
- Keep automation available through a local HTTP API when you need it.

## Product Experience

404 Downloader is built around a simple flow:

1. Start the local app.
2. Open the web UI in your browser.
3. Search for a torrent.
4. Choose the result you want to download.
5. Monitor progress and manage the download from the dashboard.

New searches refresh the search area only. Existing downloads stay visible and
manageable, whether they are active, paused, or complete.

## Quick Start

Build the app:

```bash
make build
```

The binary is written to `./bin/4dl`.

Start the web UI:

```bash
./bin/4dl
```

Open the logged local URL in your browser. By default the app listens on
`127.0.0.1:6570`.

Use a fixed listen address when needed:

```bash
./bin/4dl --listen 127.0.0.1:6570 --download-dir ~/Downloads
```

## Dashboard

The web UI focuses on day-to-day download management:

- Search results appear as candidates until you click Download.
- Download cards show status, size, seeders, peers, files, and errors.
- Runtime diagnostics help explain what the BitTorrent engine is doing.
- Completed and paused downloads remain available after new searches.
- Download tasks, metainfo, and torrent resume metadata are persisted outside
  `--download-dir`; unfinished tasks are restored on restart.

## Configuration

Common options:

```text
--listen 127.0.0.1:6570
--download-dir ~/Downloads
--state-dir ~/.config/4dl/custom-state
--torrent-listen :42069
--limit-size 50
--timeout 8s
```

`--download-dir` is optional and defaults to `~/Downloads`. It is the global
default download directory; individual tasks may carry their own path through
the HTTP API, matching Gopeed's `DownloadDir` plus task `Options.Path` model.

`--state-dir` is optional. When omitted, 4dl uses the OS config directory for
the app, such as `~/.config/4dl` on Linux. Application state, fetched metainfo,
and torrent client metadata are stored there, outside the torrent payload
namespace.

The torrent listener defaults to `:42069`. Keep that port reachable through the
host firewall and router for better peer discovery and download speed.
Torrent downloads use a speed-first profile with aggressive peer discovery and
connection limits, so expect higher file descriptor, memory, and network usage.
The downloader writes incomplete payload data to `.part` files and only promotes
them to final paths after anacrolix/torrent has accepted the pieces as complete.
Task state and fetched metainfo live in `--state-dir`; file completion is derived
from the payload layout and normal torrent verification instead of a separate
persistent piece-completion database.

Set `TORRENTCLAW_API_KEY` when TorrentClaw requires an API key.

## Local API

The app also exposes a local HTTP API for scripts and integrations. API docs are
available while the app is running:

```text
http://127.0.0.1:6570/api/docs/
```

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
