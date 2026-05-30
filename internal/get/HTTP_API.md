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
- 用户点击搜索结果的 Download 后，Web UI 调用 `POST /api/tasks` 创建持久下载任务，再调用 `POST /api/tasks/{id}/start` 开始下载；只有这一步才会进入 anacrolix/torrent。
- `--download-dir` 是全局默认下载目录，任务创建请求可通过 `path` 指定任务级下载目录；空 `path` 回落到全局默认目录，行为对齐 Gopeed 的 `DownloadDir` + `Options.Path`。
- 下载任务、状态和已取得的 metainfo 持久化在 `--state-dir`；未显式配置时使用独立的 OS config 应用目录。程序重启后会恢复未完成任务。未完成文件写入 `.part`，完成并通过 torrent 校验后才提升到最终路径。
- Web UI 通过 `GET /api/tasks/stream` 的 SSE `state` 事件刷新搜索结果和下载任务；状态变化后服务端会主动推送，1 秒定时推送只是兜底。
- 实时状态使用全局 SSE：`GET /api/tasks/stream`。
- SSE 事件名为 `state`，`data` 内容是 `AppState` JSON。
- `GET /api/tasks/stream2` 返回与 SSE `state` 事件 `data` 相同形状的当前 `AppState` JSON 快照，不打开长连接。
- `/api/docs/` 里的 Swagger UI 不能可靠 Try out SSE 长连接；调试时使用 `curl -N /api/tasks/stream` 或浏览器 `EventSource`。
- `POST /api/tasks/{id}/start` 是长任务控制接口：成功返回 `202 Accepted`，但当前实现可能等待 metadata ready 后才返回。AI Agent 应设置客户端超时，并通过 `GET /api/tasks/{id}` 或 SSE 继续观察状态。
- `POST /api/tasks/{id}/delete` 删除本地最终文件、`.part` 文件和持久任务；响应中的 task 只是删除前后的确认快照，后续列表不会再包含该任务。
- 不维护 per-task SSE；详情通过 `GET /api/tasks/{id}` 获取。
- `RuntimeSnapshot.summary.transfer` 和 `RuntimeSnapshot.peers[].transfer` 提供上下行速率，字段为 `downloadRate` 和 `uploadRate`。

## 维护规则

修改 API 路由、状态码、响应字段或枚举时，先更新 `internal/get/openapi.json`，再更新 Go/前端实现。
