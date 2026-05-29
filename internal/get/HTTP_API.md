# internal/get HTTP API

`internal/get` 的 HTTP API contract 现在以 OpenAPI 为准：

- 规范文件：`internal/get/openapi.json`
- 运行时规范地址：`GET /api/openapi.json`
- 运行时交互文档：`GET /api/docs/`

该 API 面向同进程启动的本地 Web UI，不是公开外部服务接口。

## 调用约定

- 普通接口返回 `application/json; charset=utf-8`。
- 错误响应统一为 `{"error":"message"}`，对应 OpenAPI schema `APIError`。
- 实时状态使用全局 SSE：`GET /api/torrents/stream`。
- SSE 事件名为 `state`，`data` 内容是 `AppState` JSON。
- 不维护 per-torrent SSE；详情通过 `GET /api/torrents/{id}` 获取。

## 维护规则

修改 API 路由、状态码、响应字段或枚举时，先更新 `internal/get/openapi.json`，再更新 Go/前端实现。
