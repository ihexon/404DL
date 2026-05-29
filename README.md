# 404 Downloader

404 Downloader is a local torrent discovery and download workspace. It gives you
a browser-based control panel for searching supported torrent indexes, reviewing
candidates, and managing downloads from one place.

The app runs on your machine, stores files in a directory you choose, and keeps
search results separate from active downloads until you explicitly start one.

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
./bin/4dl --save-to ~/Downloads
```

Open the logged local URL in your browser. By default the app listens on
`127.0.0.1:6570`.

Use a fixed listen address when needed:

```bash
./bin/4dl --listen 127.0.0.1:6570 --save-to ~/Downloads
```

## Dashboard

The web UI focuses on day-to-day download management:

- Search results appear as candidates until you click Download.
- Download cards show status, size, seeders, peers, files, and errors.
- Runtime diagnostics help explain what the BitTorrent engine is doing.
- Completed and paused downloads remain available after new searches.

## Configuration

Common options:

```text
--listen 127.0.0.1:6570
--save-to ~/Downloads
--torrent-listen :42069
--limit-size 50
--timeout 8s
```

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
