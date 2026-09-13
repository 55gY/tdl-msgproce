# Agent 规范

## 项目职责

`tdl-msgproce` 是 Telegram Bot 和消息监听客户端，负责从 Telegram 消息中提取链接，将内容分类为 Telegram 转发链接、订阅地址或代理节点 URI，并提交到配置的 `subs-check` API。

## API 边界

- Bot 只负责传输和流程编排，不负责代理协议解析。
- 订阅地址和节点 URI 在去除首尾空白、完成去重后，必须以原始内容提交给 API。
- 不得在本项目中解码、重新编码、规范化、校验或转换 `vmess://`、`ss://`、`ssr://`、`vless://`、`trojan://` 及其他代理协议。
- 不得重复实现 Mihomo、Clash 或 Shadowrocket 的解析逻辑。
- 协议解析、兼容处理、校验、测试、去重和持久化统一归属 `github.com/55gY/subs-check`。
- 除非 API 契约明确变化，否则保持 `bot.forward_target` 和现有订阅/节点分类行为不变。

## 实现规则

1. 复用现有的 `monitor.filters.subs` 和 `monitor.filters.ss` 配置进行分类。
2. 构造 `/api/config/add` 请求时必须保留原始链接内容。
3. 如果 API 返回解析错误，应向用户展示后端错误，并修复后端，不得增加客户端协议临时补丁。
4. Bot 的改动应限制在链接提取、批量处理、请求传输、进度和用户提示范围内。
5. 未经用户明确要求，不得提交或推送代码。
6. 代码注释、提交信息、变更说明和面向开发者的文档必须使用中文。

## 验证要求

修改后执行：

```bash
gofmt -w <修改过的 Go 文件>
go test ./...
go vet ./...
go build ./...
```

修改 API 请求格式时，必须分别验证订阅请求（`sub_url`）和节点请求（`ss`）是否符合 `subs-check` API 契约。
