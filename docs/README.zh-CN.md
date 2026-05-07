# ocgo2cli

[English](../README.md) | [中文](README.zh-CN.md)

将 [OpenCode Go](https://opencode.ai) 订阅转换为标准 Anthropic Messages API 格式，供 [Claude Code](https://github.com/anthropics/claude-code) / [Codex](https://github.com/openai/codex)（开发中）使用。

## 这是什么

OpenCode Go 提供的是 OpenAI Chat Completions 格式的 API，但 Claude Code 说的是 Anthropic Messages API。ocgo2cli 在中间做翻译官——Claude Code 发来 Anthropic 格式请求，ocgo2cli 转成 OpenAI 格式发给 OpenCode Go，再把响应转回 Anthropic 格式返回。

一句话：**用 OpenCode Go 的订阅跑 Claude Code**。

## 完整配置

推荐用 DeepSeek V4 Pro 处理复杂任务（映射 sonnet + opus），DeepSeek V4 Flash 处理轻量任务（映射 haiku）：

```json
{
  "listen": "127.0.0.1:3457",
  "opencode_base_url": "https://opencode.ai/zen/go/v1/chat/completions",
  "opencode_anthropic_base_url": "https://opencode.ai/zen/go/v1/messages",
  "api_key": "${OC_API_KEY}",
  "models": {
    "claude-sonnet-4-20250514": {
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 8192,
      "reasoning_effort": "max",
      "thinking": {"type": "enabled"}
    },
    "claude-opus-4-6-20250514": {
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 16384
    },
    "claude-haiku-4-5-20250514": {
      "model_id": "deepseek-v4-flash",
      "temperature": 0.5,
      "max_tokens": 4096
    }
  }
}
```

配置逻辑：`claude-sonnet-4-20250514` 和 `claude-opus-4-6-20250514` 都指向 `deepseek-v4-pro`（复杂推理），`claude-haiku-4-5-20250514` 指向 `deepseek-v4-flash`（快速轻量）。Claude Code 选模型时自动路由到对应的后端模型。

## 快速开始

### 安装

```bash
git clone git@github.com:SurgeSeeker/ocgo2cli.git
cd ocgo2cli
make build
```

### 配置

```bash
mkdir -p ~/.config/ocgo2cli
cp config.example.json ~/.config/ocgo2cli/config.json
# 编辑 config.json，写入你的 API key
```

### 运行

```bash
./bin/ocgo2cli run        # 前台运行，适合调试
./bin/ocgo2cli start      # 后台守护进程
./bin/ocgo2cli status     # 查看状态
./bin/ocgo2cli stop       # 停止
```

### 接入 Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:3457"
  }
}
```

> ⚠️ Claude Code 不支持从 shell 环境变量读取 `ANTHROPIC_BASE_URL`，必须通过 `settings.json` 的 `env` 字段配置。

## 守护进程

```bash
ocgo2cli install    # 安装为用户级服务（免 root）
ocgo2cli uninstall  # 卸载
ocgo2cli start      # 启动
ocgo2cli stop       # 停止
ocgo2cli restart    # 重启
ocgo2cli status     # 状态
```

跨平台支持：Linux（systemd）、macOS（launchd）、Windows（SCM）。

## 配置参考

| 字段 | 说明 |
|------|------|
| `listen` | 监听地址，默认 `127.0.0.1:3457` |
| `opencode_base_url` | OpenAI 兼容 API 地址 |
| `opencode_anthropic_base_url` | Anthropic 原生 API 地址（MiniMax 模型直通） |
| `api_key` | API 密钥，支持 `${变量名}` 引用环境变量 |
| `models.<模型名>.model_id` | 实际调用的后端模型 ID |
| `models.<模型名>.temperature` | 覆盖温度参数 |
| `models.<模型名>.max_tokens` | 覆盖最大 token 数 |
| `models.<模型名>.reasoning_effort` | 推理深度（DeepSeek thinking 模式下有效） |
| `models.<模型名>.thinking` | thinking 模式开关：`{"type":"enabled"}` 或 `{"type":"disabled"}` |

## 功能要点

- **Anthropic ↔ OpenAI 格式互转** — 完整保真，thinking/reasoning_content 双向透传
- **Anthropic 原生模型直通** — MiniMax M2.5/M2.7 等原生 Anthropic 格式的模型零转换转发
- **SSE 流式** — 实时流式响应，reasoning 和 text 块之间正确切换
- **环境变量插值** — 配置文件中 `${VAR}` 自动展开，API key 不入仓库
- **跨平台守护进程** — 基于 kardianos/service，用户级安装无需 root

## 构建与测试

```bash
make build    # 编译 → bin/ocgo2cli
make test     # 跑测试（含 -race 竞态检测）
make lint     # go vet 静态检查
make clean    # 清理构建产物
```

## 常见问题

### Claude Code 报 401 / 连接拒绝

1. 确认 ocgo2cli 已启动：`ocgo2cli status`
2. 确认 `ANTHROPIC_BASE_URL` 在 `settings.json` 的 `env` 中配置（不是 shell export）
3. 确认 `api_key` 已正确设置，`${OC_API_KEY}` 对应的环境变量存在

### Thinking 模式不生效

DeepSeek 的 thinking 模式要求对话历史中每个 assistant 消息都带 `reasoning_content`。如果报 400 错误，检查是否缺了 `reasoning_content` 字段。ocgo2cli 会自动补占位符。

## 致谢

格式转换逻辑基于 [oc-go-cc](https://github.com/nousresearch/oc-go-cc)（Nous Research）。

## 许可证

GNU Affero General Public License v3.0 — 详见 [LICENSE](LICENSE)。
