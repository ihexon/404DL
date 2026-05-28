# internal/get HTTP API

本文档记录 `internal/get` 本地 Web UI 使用的 HTTP API。该 API 面向同进程启动的本地页面，不是公开外部服务接口。

## 设计约定

- 所有普通接口返回 `application/json; charset=utf-8`。
- 错误响应统一为：

```json
{"error":"message"}
```

- 下载中的实时状态使用一个全局 SSE：
  - `GET /api/torrents/stream`
  - 推送事件名为 `state`
  - `data` 内容与 `GET /api/torrents` 返回的 `AppState` 同形状
- 不再使用 per-torrent SSE。详情页仍通过 `GET /api/torrents/{id}` 获取完整单项信息。

## Endpoint

### `GET /api/torrents`

返回当前 torrent 列表快照。

状态码：

- `200 OK`

响应体：`AppState`

说明：

- 列表项默认不携带 `files`，避免列表响应过重。
- 下载中的 torrent 会带 `runtime` 快照；非下载中的 torrent 通常为 `runtime.status = "inactive"`。

### `GET /api/torrents/stream`

返回全局 torrent 列表 SSE。

响应头：

```text
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

事件格式：

```text
event: state
data: <AppState JSON>
```

行为：

- 建连后立即推送一次 `state`。
- 之后每 1 秒推送一次 `state`。
- 浏览器端使用 `EventSource` 的原生自动重连能力。
- 服务端写入或 flush 失败时结束该 stream。

### `GET /api/torrents/{id}`

返回单个 torrent 的完整状态。

状态码：

- `200 OK`
- `404 Not Found`

响应体：`TorrentState`

说明：

- 用于展开单个 torrent 时加载详情。
- 当 metadata 已加载时，响应会包含 `files`。
- 若 runtime 已加载，响应会包含 `runtime.snapshot`。

### `POST /api/torrents/{id}/start`

开始或继续下载指定 torrent。

请求体：忽略，前端当前发送 `{}`。

状态码：

- `202 Accepted`
- `404 Not Found`
- `409 Conflict`

响应体：`TorrentState`

说明：

- 当前实现会等待 torrent metadata ready 后再返回，因此该请求可能阻塞一段时间。
- 成功后 `download.status` 为 `downloading`，全局 SSE 后续会持续推送进度。

### `POST /api/torrents/{id}/pause`

暂停指定 torrent 下载。

请求体：忽略，前端当前发送 `{}`。

状态码：

- `200 OK`
- `404 Not Found`
- `409 Conflict`

响应体：`TorrentState`

说明：

- 如果该 torrent 当前没有 active runtime，接口会返回当前 item 状态，不强制报错。

### `POST /api/torrents/{id}/delete`

删除指定 torrent 的本地下载数据并清理运行状态。

请求体：忽略，前端当前发送 `{}`。

状态码：

- `200 OK`
- `404 Not Found`
- `409 Conflict`

响应体：`TorrentState`

说明：

- 会取消下载、禁止继续下载数据，并尝试删除已保存文件或 `.part` 文件。
- 成功后 download 状态回到 idle。

## 数据结构

### `AppState`

```json
{
  "updated": "2026-05-28T12:00:00+08:00",
  "saveTo": "/path/to/save",
  "torrents": []
}
```

字段：

- `updated`: 服务端生成该快照的时间。
- `saveTo`: 下载保存目录。
- `torrents`: `TorrentState[]`。

### `TorrentState`

`TorrentState` 由 `TorrentItem` 加 `runtime` 组成。

主要字段：

- `id`: torrent item id。
- `title`: 标题。
- `provider`: 来源 provider。
- `bytes`: 总大小，可能省略。
- `category`: 分类，可能省略。
- `date`: 日期字符串，可能省略。
- `seeders`: 搜索结果中的 seed 数。
- `peers`: 搜索结果中的 peer 数。
- `hash`: info hash，可能省略。
- `magnetUrl`: magnet link，可能省略。
- `downloading`: 是否处于 active download。
- `download`: `DownloadView`。
- `error`: item 级错误，可能省略。
- `files`: `FileItem[]`，列表接口通常不返回，详情接口在 metadata ready 后返回。
- `runtime`: `RuntimeView`。

### `DownloadView`

```json
{
  "status": "idle",
  "completedBytes": 0,
  "bytes": 0
}
```

`status` 可选值：

- `idle`
- `downloading`
- `paused`
- `complete`

### `RuntimeView`

```json
{
  "status": "inactive"
}
```

`status` 可选值：

- `inactive`: 没有 active runtime。
- `ready`: `snapshot` 可用。
- `error`: `error` 字段描述错误。

### `RuntimeSnapshot`

下载中 runtime 的快照。

主要字段：

- `id`: torrent item id。
- `updated`: 快照时间。
- `summary`: `RuntimeSummary`。
- `peers`: 最多 30 个 active peer，按下载速率排序。
- `pieceRuns`: piece state run-length 数据。
- `dht`: DHT server 状态。
- `events`: 最近 runtime 事件。

### `RuntimeSummary`

主要字段：

- `infoHash`
- `name`
- `metadataReady`
- `bytesCompleted`
- `length`
- `downloadRate`
- `pendingPeers`
- `activePeers`
- `connectedSeeders`
- `halfOpenPeers`
- `piecesComplete`
- `numPieces`
- `chunksReadUseful`
- `chunksReadWasted`
- `bytesReadUsefulData`
- `bytesWrittenData`

### `FileItem`

```json
{
  "path": "file.mkv",
  "bytes": 100,
  "completedBytes": 50,
  "savePath": "/downloads/file.mkv",
  "status": "downloading"
}
```

`status` 可选值：

- `idle`
- `downloading`
- `complete`

## 前端推荐调用方式

1. 页面初始化时直接启动一个全局 `EventSource("/api/torrents/stream")`。
2. SSE 建连后服务端会立即推送第一条 `state`，前端用这条 `AppState` 完成首屏初始化。
3. 后续收到 `state` 事件后，用新的 `AppState` 更新列表和下载中 runtime。
4. 用户展开单项时调用 `GET /api/torrents/{id}` 加载 files 和完整详情。
5. 用户操作 start/pause/delete 时调用对应 POST，并用返回的 `TorrentState` 合并本地状态。

## 设计边界

- SSE 只负责推送 `AppState` 快照，不定义增量 patch 协议。
- 不维护每个 torrent 的独立 stream。
- 不在前端额外做高频 REST polling；实时更新来源是全局 SSE。
- 如果将来需要真正事件日志流，可在不影响当前 snapshot stream 的前提下新增独立事件接口。
