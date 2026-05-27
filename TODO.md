# TODO

## Runtime Diagnostics Refactor

- Replace aggregated piece runs with real torrent-level pieces:
  - Backend returns one `RuntimePiece` per real BitTorrent piece.
  - Piece state comes from anacrolix `Torrent.PieceStateRuns()`.
  - The UI renders one box per real piece, never an aggregated box.
- Track piece download source:
  - Record the latest peer that supplied useful data for each piece from anacrolix callbacks.
  - Expose peer address, client, source, and timestamp on the piece.
  - Label this as the latest useful peer because one piece can be assembled from multiple peers.
- Move runtime updates from polling to SSE:
  - Keep `GET /api/state` as the canonical read model.
  - Add `GET /api/state/stream` for live full-state snapshots.
  - Make the stream cancel cleanly with request context and manager shutdown.
- Keep the frontend responsive:
  - Render the piece grid with a fixed-height scroll container.
  - Use virtualized rows so large torrents do not create excessive DOM nodes.
  - Show piece details on hover without changing the one-box-per-piece contract.
- Rebuild the embedded web assets after frontend changes.
- Rewrite `README.md` so it describes the project, CLI, API, get UI, runtime diagnostics, and configuration.
- Commit all changes with `git commit -s -S` using the repository commit conventions.
