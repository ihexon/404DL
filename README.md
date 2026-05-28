# 404 Downloader

404 Downloader is a local file search and download tool.

It helps you search files from supported providers, review the results, and pass
selected results into a local browser UI for downloading. The project is built
around a small CLI named `4dl`, with an optional HTTP API mode for automation or
interactive API exploration.

## What It Does

- Searches files from multiple providers.
- Returns structured search results that can be piped into other commands.
- Opens a local web UI for managing selected downloads.
- Supports provider filtering when you want to narrow a search.
- Can run as a lightweight local Search API server.

## Quick Start

Build the CLI:

```bash
make build
```

The binary is written to `./bin/4dl`.

Search files:

```bash
./bin/4dl search "linux interface"
```

Limit returned results:

```bash
./bin/4dl search --limit-size 3 "linux interface"
```

Search only selected providers:

```bash
./bin/4dl search --provider knaben --provider torrentclaw "linux interface"
```

Send search results into the download UI:

```bash
./bin/4dl search "linux interface" | ./bin/4dl get --stdin --save-to ./downloads
```

Use saved search results:

```bash
./bin/4dl get --input results.json --save-to ./downloads
```

By default, `search` runs independently. It starts an embedded Search API server
for the current command, queries all providers, prints JSON, and exits.

## Local Web UI

The `get` command opens a local web UI for selected search results. From there
you can review items, start or pause downloads, delete downloaded data, and
inspect download progress.

Example:

```bash
./bin/4dl search "linux interface" | ./bin/4dl get --stdin --save-to ./downloads
```

`--save-to` is required, so the download directory is always explicit:

```bash
./bin/4dl get --input results.json --save-to ~/Downloads
```

## Search API Mode

Run a long-lived local Search API server when you want to call 404 Downloader
from scripts, tools, or the interactive API docs:

```bash
./bin/4dl server
```

Open the API docs:

```text
http://127.0.0.1:6567/docs
```

Search through the API:

```bash
curl --noproxy '*' 'http://127.0.0.1:6567/v1/search?q=linux%20interface&limit=3'
```

The CLI can also call an existing API server explicitly:

```bash
./bin/4dl search --server-url http://127.0.0.1:6567 "linux interface"
```

## Configuration

Common options:

```text
search --limit-size 50 --timeout 8s
server --listen 127.0.0.1:6567 --limit-size 50 --timeout 8s
get --listen 127.0.0.1:6570 --save-to ./downloads
```

Environment variables are reserved for sensitive values:

```text
TORRENTCLAW_API_KEY=
FOURDL_CRYKEY=
```

`TORRENTCLAW_API_KEY` is used when TorrentClaw requires an API key.

`FOURDL_CRYKEY` enables AES-256-GCM encryption for Search API response bodies.
When `search` has this key locally, it requests an encrypted response and
refuses plaintext responses. When `get` has this key locally, it decrypts its
search-result input before reading JSON. It must be exactly 32 bytes.

## Docker

Build:

```bash
docker build -t 404-downloader .
```

Run the API server:

```bash
docker run --rm -p 6567:8080 404-downloader
```

Run on a custom address inside the container:

```bash
docker run --rm -p 18080:18080 404-downloader server --listen :18080
```
