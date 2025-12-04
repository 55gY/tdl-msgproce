# tdl-msgproce

**基于 tdl 的 Telegram 消息监听与转发扩展**

[![GitHub](https://img.shields.io/badge/GitHub-55gY%2Ftdl--msgproce-blue)](https://github.com/55gY/tdl-msgproce)

## 📦 项目简介

`tdl-msgproce` 是 [tdl](https://github.com/iyear/tdl) 的扩展程序，融合了消息监听和转发两大功能：

1. **消息监听**：实时监听 Telegram 频道，过滤关键词并提交到订阅 API
2. **消息转发**：定时转发指定频道/链接的消息到目标聊天

### ✨ 核心特性

- ✅ **单 session 运行** - 共享 tdl session，避免会话冲突
- ✅ **资源占用低** - 单进程运行，内存占用 < 100MB
- ✅ **统一管理** - 配置、日志集中管理
- ✅ **原生集成** - 直接使用 tdl 功能，无需额外脚本

## 🔗 相关项目

本项目是系列项目之一，各项目关系如下：

| 项目 | 说明 | 依赖 tdl | Session 数量 | GitHub |
|------|------|----------|--------------|--------|
| [go-TelegramMessage](https://github.com/55gY/go-TelegramMessage) | 纯 Go 实现的消息监听器 | ❌ 独立运行 | 1 | [![GitHub](https://img.shields.io/badge/GitHub-repo-blue)](https://github.com/55gY/go-TelegramMessage) |
| [go-bot](https://github.com/55gY/go-bot) | Telegram 转发机器人 | ❌ 独立运行 | 1 | [![GitHub](https://img.shields.io/badge/GitHub-repo-blue)](https://github.com/55gY/go-bot) |
| [ext-msgwait](https://github.com/55gY/ext-msgwait) | 基于 go-TelegramMessage + tdl | ✅ 混合模式 | 2 (冲突) | [![GitHub](https://img.shields.io/badge/GitHub-repo-blue)](https://github.com/55gY/ext-msgwait) |
| **ext_msgproce** (本项目) | 完全基于 tdl 的融合版 | ✅ 纯 tdl 扩展 | 1 | [![GitHub](https://img.shields.io/badge/GitHub-repo-blue)](https://github.com/55gY/tdl-msgproce) |

### 📊 项目选择指南

- **推荐使用 ext_msgproce (本项目)**：单 session，资源占用低，功能完整
- **ext-msgwait**：历史版本，需要 2 个 session，可能有冲突
- **go-TelegramMessage / go-bot**：独立运行，适合不想安装 tdl 的场景

## 功能特性

### 1. 消息监听功能

- 监听指定 Telegram 频道的实时消息
- 支持关键词过滤（如包含链接）
- 二次内容过滤（如必须包含"投稿"、"订阅"）
- 白名单频道（跳过二次过滤）
- 链接黑名单（过滤图片、特定域名等）
- 自动提取并发送有效链接到订阅 API
- 启动时可选获取历史消息（最近100条）

### 2. 转发功能

- 支持从文件读取转发列表
- 支持单个频道/链接转发
- 可配置转发间隔（定时任务）
- clone 模式：完整克隆消息格式
- 统一转发到指定目标聊天

### 3. 融合优势

- 不需要多个 tdl 进程
- 避免 BoltDB session 文件锁冲突
- 一次登录，多个任务同时运行
- 统一的心跳监控和日志输出

## 配置文件

`config.yaml` 包含两部分配置：

```yaml
# ==================== 消息监听配置 ====================
monitor:
  enabled: true  # 是否启用
  subscription_api:
    host: "your-api-host"
    api_key: "your-key"
  channels: [...]  # 监听的频道ID
  filters: {...}   # 过滤规则

# ==================== 转发任务配置 ====================
forward:
  enabled: true    # 是否启用
  target_chat: 1838605845  # 转发目标
  mode: clone      # 转发模式
  tasks:
    - type: file   # 从文件读取
      path: "/path/to/list"
      interval: 300  # 每5分钟执行一次
```

## 🚀 快速安装

### 方法一：自动安装（推荐）

```bash
# 下载安装脚本
curl -sSL https://raw.githubusercontent.com/55gY/tdl-msgproce/main/install.sh -o install.sh
chmod +x install.sh

# 运行安装
./install.sh
```

安装脚本会自动：
- ✅ 检测系统架构（Linux/Darwin, amd64/arm64/armv7）
- ✅ 从 GitHub Releases 下载 tdl 和 tdl-msgproce
- ✅ 安装到 `~/.tdl/extensions/` 目录
- ✅ 创建配置文件模板
- ✅ 引导完成登录和配置

### 方法二：下载预编译文件

从 [Releases](https://github.com/55gY/tdl-msgproce/releases) 下载对应平台的二进制文件：

```bash
# Linux AMD64
wget https://github.com/55gY/tdl-msgproce/releases/latest/download/tdl-msgproce_linux_amd64
chmod +x tdl-msgproce_linux_amd64
mv tdl-msgproce_linux_amd64 ~/.tdl/extensions/tdl-msgproce

# Windows AMD64
# 下载 tdl-msgproce_windows_amd64.exe
# 放到 %USERPROFILE%\.tdl\extensions\ 目录
```

### 方法三：手动编译

```bash
# 1. 克隆仓库
git clone https://github.com/55gY/tdl-msgproce.git
cd tdl-msgproce

# 2. 编译
go mod tidy
go build -ldflags="-s -w" -o tdl-msgproce

# 3. 安装
mkdir -p ~/.tdl/extensions/tdl-msgproce
cp tdl-msgproce ~/.tdl/extensions/tdl-msgproce/

# 4. 配置
mkdir -p ~/.tdl/extensions/data/msgproce
cp config.yaml ~/.tdl/extensions/data/msgproce/
nano ~/.tdl/extensions/data/msgproce/config.yaml
```

### 交叉编译（低内存服务器）

如果服务器内存不足，可以在本地编译后上传：

**Windows PowerShell:**
```powershell
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags="-s -w" -o tdl-msgproce
```

**Linux/Mac:**
```bash
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o tdl-msgproce
```

## 📖 使用方法

### 1. 安装 tdl（如未安装）

安装脚本会自动下载，或手动安装：

```bash
# Linux
curl -sSL https://github.com/iyear/tdl/releases/latest/download/tdl_linux_amd64.tar.gz | tar xz
mv tdl ~/.tdl/tdl
chmod +x ~/.tdl/tdl
```

### 2. 登录 Telegram

使用二维码登录（推荐）：

```bash
~/.tdl/tdl login -n default -T qr
```

或使用手机号登录：

```bash
~/.tdl/tdl login -n default
```

### 3. 安装扩展到 tdl

```bash
# 安装扩展（首次使用必须执行）
~/.tdl/tdl extension install --force ~/.tdl/extensions/tdl-msgproce/tdl-msgproce

# 验证安装
~/.tdl/tdl extension list
```

### 4. 配置扩展

编辑配置文件：

```bash
nano ~/.tdl/extensions/data/msgproce/config.yaml
```

**必须配置的项目：**

```yaml
# Bot Token（如需转发功能）
bot:
  token: "YOUR_BOT_TOKEN"  # 从 @BotFather 获取
  
# 订阅 API（如需监听功能）
subscription_api:
  api_key: "YOUR_API_KEY"
  add_url: "http://your-api.com:port/api/subscription/add"
  
# 监听的频道
monitor:
  channels:
    - 1234567890  # 使用 tdl chat ls 查看频道ID
```

### 5. 运行扩展

```bash
# 标准运行
~/.tdl/tdl -n default msgproce

# 后台运行（systemd）
./install.sh  # 选择"安装 systemd 服务"

# 查看日志
tail -f ~/.tdl/extensions/data/msgproce/log/latest.log
```

## 配置示例

### 完整配置文件

```yaml
# 消息监听配置
monitor:
  enabled: true
  subscription_api:
    host: "113.194.190.201:26908"
    api_key: "123456"
  features:
    fetch_history_enabled: true
  channels:
    - 2582776039
    - 1338209352
  whitelist_channels:
    - 1313311705
  filters:
    keywords:
      - "https://"
      - "http://"
    content_filter:
      - "投稿"
      - "订阅"
    link_blacklist:
      - "register"
      - "t.me"
      - ".jpg"

# 转发配置
forward:
  enabled: true
  target_chat: 1838605845
  mode: clone
  tasks:
    # 从文件读取，每5分钟执行
    - type: file
      path: "/root/go-bot/default"
      enabled: true
      interval: 300
    
    # 单个频道，只执行一次
    - type: channel
      from: "@channel_username"
      enabled: true
      interval: 0
```

## 获取频道ID

```bash
~/.tdl/tdl -n default chat ls
```

## 🛠️ 管理脚本

`install.sh` 提供完整的管理功能：

```bash
./install.sh
```

**菜单选项：**

1. **完整安装** - 一键安装 tdl + msgproce + 配置
2. **仅安装 tdl** - 单独安装 tdl
3. **仅安装 msgproce** - 单独安装扩展
4. **控制台启动** - 前台运行，方便调试
5. **安装 systemd 服务** - 后台运行，开机自启
6. **停止运行** - 停止所有相关进程
7. **重启服务** - 重启后台服务
8. **环境检测** - 检查安装状态
9. **查看状态** - 查看运行状态和资源占用
10. **查看日志** - 查看运行日志
11. **编辑配置** - 编辑配置文件
12. **完全卸载** - 删除所有组件

## 🔧 systemd 服务管理

安装为系统服务后：

```bash
# 启动
systemctl start tdl-msgproce

# 停止
systemctl stop tdl-msgproce

# 重启
systemctl restart tdl-msgproce

# 状态
systemctl status tdl-msgproce

# 日志
journalctl -u tdl-msgproce -f

# 开机自启
systemctl enable tdl-msgproce
```

## 🐛 故障排查

### 扩展未找到

```bash
# 检查文件是否存在
ls -la ~/.tdl/extensions/tdl-msgproce/tdl-msgproce

# 检查权限
chmod +x ~/.tdl/extensions/tdl-msgproce/tdl-msgproce

# 查看扩展列表
~/.tdl/tdl extension list
```

### 配置错误

功能会自动禁用如果配置不完整：

```bash
# 查看日志
tail -50 ~/.tdl/extensions/data/msgproce/log/latest.log

# 检查配置
cat ~/.tdl/extensions/data/msgproce/config.yaml
```

### Session 冲突

确保没有多个进程使用同一个 namespace：

```bash
ps aux | grep tdl
```

### 下载失败

如果自动下载失败，手动下载：

- tdl: https://github.com/iyear/tdl/releases
- msgproce: https://github.com/55gY/tdl-msgproce/releases

## 📊 性能优化

- **编译优化**：使用 `-ldflags="-s -w"` 减小文件体积
- **内存占用**：约 50-100MB
- **CPU 占用**：空闲时 < 1%
- **转发间隔**：建议 ≥ 300秒，避免 API 限制

## 📦 版本发布

查看 [Releases](https://github.com/55gY/tdl-msgproce/releases) 获取最新版本。

自动构建支持：
- Linux AMD64
- Windows AMD64

每次推送版本标签（如 `v1.0.0`）会自动触发构建和发布。

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 开源协议

MIT License

## 🔗 相关链接

- **tdl 项目**: https://github.com/iyear/tdl
- **go-TelegramMessage**: https://github.com/55gY/go-TelegramMessage
- **go-bot**: https://github.com/55gY/go-bot  
- **ext-msgwait**: https://github.com/55gY/ext-msgwait
