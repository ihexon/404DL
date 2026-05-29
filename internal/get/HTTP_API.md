# internal/get HTTP API

`internal/get` 的 HTTP API contract 现在以 OpenAPI 为准：

- 规范文件：`internal/get/openapi.json`
- 运行时规范地址：`GET /api/openapi.json`
- 运行时交互文档：`GET /api/docs/`

该 API 面向同进程启动的本地 Web UI，不是公开外部服务接口。

## 调用约定

- 普通接口返回 `application/json; charset=utf-8`。
- 错误响应统一为 `{"error":"message"}`，对应 OpenAPI schema `APIError`。
- 健康检查接口为 `GET /api/healthz`，成功返回 `{"status":"ok"}`。
- 搜索接口为 `POST /api/search`，请求体为 `SearchRequest`，返回并保存已按 hash 归一化、去重后的 `SearchResult[]`；没有 hash 的 provider 结果会被丢弃。搜索结果只是候选项，不会进入 anacrolix/torrent。
- 用户点击搜索结果的 Download 后，Web UI 调用 `POST /api/torrents` 创建下载任务，再调用 `POST /api/torrents/{id}/start` 开始下载；只有这一步才会进入 anacrolix/torrent。
- Web UI 通过 `GET /api/torrents/stream` 的 SSE `state` 事件刷新搜索结果和下载任务；状态变化后服务端会主动推送，1 秒定时推送只是兜底。
- 实时状态使用全局 SSE：`GET /api/torrents/stream`。
- SSE 事件名为 `state`，`data` 内容是 `AppState` JSON。
- `GET /api/torrents/stream2` 返回与 SSE `state` 事件 `data` 相同形状的当前 `AppState` JSON 快照，不打开长连接。
- `/api/docs/` 里的 Swagger UI 不能可靠 Try out SSE 长连接；调试时使用 `curl -N /api/torrents/stream` 或浏览器 `EventSource`。
- `POST /api/torrents/{id}/start` 是长任务控制接口：成功返回 `202 Accepted`，但当前实现可能等待 metadata ready 后才返回。AI Agent 应设置客户端超时，并通过 `GET /api/torrents/{id}` 或 SSE 继续观察状态。
- 不维护 per-torrent SSE；详情通过 `GET /api/torrents/{id}` 获取。

## 维护规则

修改 API 路由、状态码、响应字段或枚举时，先更新 `internal/get/openapi.json`，再更新 Go/前端实现。
