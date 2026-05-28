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
./bin/4dl server --listen 127.0.0.1:6567
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

### Encrypted Search API Responses

`FOURDL_CRYKEY` is a 32-byte AES-256-GCM key for encrypting complete Search API
response bodies. It does not encrypt individual `magnetUrl` fields.

Server behavior:

```bash
FOURDL_CRYKEY=12345678901234567890123456789012 ./bin/4dl server
```

When the server has `FOURDL_CRYKEY`, it can return encrypted `/v1/search`
responses. Encryption is used only when the client asks for it with
`X-4DL-Require-Encrypted: true`. If the client requires encryption but the
server has no key, the server returns HTTP 412.

`search` behavior:

```bash
FOURDL_CRYKEY=12345678901234567890123456789012 \
  ./bin/4dl search --server-url http://127.0.0.1:6567 "linux interface"
```

When `search` has `FOURDL_CRYKEY`, it requires an encrypted API response,
decrypts it locally, and prints normal JSON. When `search` has no key, it does
not request encryption and expects normal JSON.

`get` behavior:

```bash
FOURDL_CRYKEY=12345678901234567890123456789012 \
  ./bin/4dl get --input encrypted-results.txt --save-to ./downloads
```

When `get` has `FOURDL_CRYKEY`, it decrypts its whole input first, then reads
JSON. When `get` has no key, it reads plaintext JSON directly.

For the normal pipeline, let `search` decrypt the API response and pass plaintext
JSON to `get`:

```bash
FOURDL_CRYKEY=12345678901234567890123456789012 \
  ./bin/4dl search "linux interface" | ./bin/4dl get --stdin --save-to ./downloads
```

If `FOURDL_CRYKEY` is exported in your shell, unset it for `get` when feeding it
the plaintext output of `search`:

```bash
./bin/4dl search "linux interface" | env -u FOURDL_CRYKEY ./bin/4dl get --stdin --save-to ./downloads
```

## Configuration

Common options:

```text
search --limit-size 50 --timeout 8s
server --listen 127.0.0.1:6567 --limit-size 50 --timeout 8s
get --listen 127.0.0.1:6570 --torrent-listen :42069 --save-to ./downloads
```

Environment variables are reserved for sensitive values:

```text
TORRENTCLAW_API_KEY=
FOURDL_CRYKEY=
```

`TORRENTCLAW_API_KEY` is used when TorrentClaw requires an API key.

`FOURDL_CRYKEY` enables AES-256-GCM encryption for whole Search API response
bodies. It must be exactly 32 bytes.

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
