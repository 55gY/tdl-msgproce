# tdl-msgproce

**基于 tdl 的 Telegram 消息处理扩展**

[![GitHub](https://img.shields.io/badge/GitHub-55gY%2Ftdl--msgproce-blue)](https://github.com/55gY/tdl-msgproce)
[![Release](https://img.shields.io/github/v/release/55gY/tdl-msgproce)](https://github.com/55gY/tdl-msgproce/releases)
[![License](https://img.shields.io/github/license/55gY/tdl-msgproce)](LICENSE)

## 📦 项目简介

`tdl-msgproce` 是 [tdl](https://github.com/iyear/tdl) 的扩展程序，集成三大核心功能：

1. **消息监听** - 实时监听 Telegram 频道，智能过滤并提交到订阅 API
2. **Bot 交互** - Telegram Bot 支持，接收和处理用户命令
3. **消息转发** - 自动转发频道消息到指定目标（支持 clone/copy 模式）

### ✨ 核心特性

- ✅ **单 session 运行** - 共享 tdl session，避免会话冲突
- ✅ **资源占用低** - 单进程运行，内存占用 < 100MB
- ✅ **智能过滤** - 支持订阅链接和节点链接双重过滤
- ✅ **自动安装** - 提供完整的安装管理脚本
- ✅ **统一管理** - 配置、日志集中管理
- ✅ **原生集成** - 直接使用 tdl 功能，无需额外脚本
- ✅ **自动发布** - 推送包含版本号的提交即可自动发布 Release

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

### 1. 消息监听功能 📝

- ✅ 实时监听指定 Telegram 频道消息
- ✅ **双重过滤机制**：
  - **订阅链接过滤**：匹配 `http://` 或 `https://`，进行二次内容过滤
  - **节点链接过滤**：匹配 `vmess://`、`vless://`、`ss://`、`trojan://` 等协议，无需二次过滤
- ✅ 二次内容过滤（订阅链接专用，检查是否包含"投稿"、"订阅"等关键词）
- ✅ 白名单频道机制（跳过二次内容过滤）
- ✅ 链接黑名单（过滤图片、特定域名等）
- ✅ 自动提取并提交有效链接到订阅 API
- ✅ 启动时可选获取历史消息（可配置数量）
- ✅ **全频道/群组节点监听**（不受监听频道列表限制）

### 2. Bot 交互功能 🤖

- ✅ Telegram Bot 支持
- ✅ 用户权限控制（允许列表）
- ✅ 接收用户发送的消息/链接
- ✅ 自动转发到指定目标
- ✅ 支持 clone 和 copy 两种转发模式

### 3. 消息转发功能 📤

- ✅ 支持从文件读取转发列表
- ✅ 支持单个频道/链接转发
- ✅ 可配置转发间隔（定时任务）
- ✅ clone 模式：完整克隆消息格式
- ✅ copy 模式：简单复制
- ✅ 统一转发到指定目标聊天

### 4. 融合优势 🎯

- ✅ 不需要多个 tdl 进程
- ✅ 避免 BoltDB session 文件锁冲突
- ✅ 一次登录，多个任务同时运行
- ✅ 统一的配置文件和日志输出
- ✅ 自动化安装和管理脚本

## 配置文件

`config.yaml` 包含三部分配置：

```yaml
# ==================== Telegram Bot 配置 ====================
bot:
  enabled: true              # 是否启用 Bot 功能
  token: "YOUR_BOT_TOKEN"   # 从 @BotFather 获取
  allowed_users: []          # 允许使用的用户ID列表（空=所有人）
  forward_target: 1838605845 # 转发目标 chat ID
  forward_mode: "clone"      # clone 或 copy

# ==================== 消息监听配置 ====================
monitor:
  enabled: true  # 是否启用监听功能
  
  # 订阅 API 配置
  subscription_api:
    api_key: "123456"
    add_url: "http://your-api.com:port/api/subscription/add"
  
  # 历史消息功能
  features:
    fetch_history_count: 100  # >0 开启并获取指定数量，<=0 关闭
  
  # 监听的频道ID列表
  channels:
    - 2582776039
    - 1338209352
  
  # 白名单频道（跳过二次内容过滤）
  whitelist_channels:
    - 1313311705
  
  # 过滤配置
  filters:
    # 订阅格式过滤（需要二次内容过滤）
    subs:
      - "https://"
      - "http://"
    
    # 节点格式过滤（全频道监听，无需二次过滤）
    ss:
      - "vmess://"
      - "vless://"
      - "ss://"
      - "ssr://"
      - "trojan://"
      - "hysteria://"
      - "hysteria2://"
      - "hy2://"
      - "tuic://"
      - "juicity://"
    
    # 内容过滤（订阅链接的二次过滤）
    content_filter:
      - "投稿"
      - "订阅"
    
    # 链接黑名单
    link_blacklist:
      - "register"
      - "t.me"
      - ".jpg"
      - ".jpeg"
      - ".png"
```

### 过滤机制说明

1. **订阅链接过滤**（`subs`）：
   - 监听 `channels` 列表中的频道
   - 匹配 `http://` 或 `https://` 开头的链接
   - 需要通过二次内容过滤（除非在白名单中）
   - 检查消息是否包含 `content_filter` 中的关键词

2. **节点链接过滤**（`ss`）：
   - 监听**全部**频道/群组（不受 `channels` 限制）
   - 匹配指定协议的节点链接
   - 无需二次内容过滤，直接提交
   - 适用于 vmess、vless、ss、trojan 等节点分享

3. **白名单机制**：
   - `whitelist_channels` 中的频道跳过二次内容过滤
   - 所有匹配订阅格式的链接直接提交

## 🚀 快速安装

### 方法一：一键安装脚本（推荐）

```bash
# 下载并运行安装脚本
curl -sSL https://raw.githubusercontent.com/55gY/tdl-msgproce/main/install.sh -o install.sh
chmod +x install.sh
./install.sh
```

**安装脚本功能：**
- ✅ 自动检测系统架构（Linux/Darwin, amd64/arm64）
- ✅ 从 GitHub Releases 自动下载 tdl 和 tdl-msgproce
- ✅ 安装到标准目录（`~/.tdl/extensions/`）
- ✅ 创建配置文件模板
- ✅ 引导完成 Telegram 登录
- ✅ 提供交互式菜单管理
- ✅ 支持 systemd 服务安装

**安装脚本菜单：**
1. 完整安装（tdl + msgproce + 配置）
2. 仅安装 tdl
3. 仅安装 msgproce 扩展
4. 控制台启动（前台运行）
5. 安装 systemd 服务（后台运行）
6. 停止运行
7. 重启服务
8. 环境检测
9. 查看状态
10. 查看日志
11. 编辑配置
12. 完全卸载

### 方法二：下载预编译文件

从 [Releases](https://github.com/55gY/tdl-msgproce/releases) 下载对应平台的二进制文件：

**Linux AMD64:**
```bash
wget https://github.com/55gY/tdl-msgproce/releases/latest/download/tdl-msgproce_linux_amd64
chmod +x tdl-msgproce_linux_amd64
mkdir -p ~/.tdl/extensions/tdl-msgproce
mv tdl-msgproce_linux_amd64 ~/.tdl/extensions/tdl-msgproce/tdl-msgproce
```

**Windows AMD64:**
```powershell
# 下载文件
Invoke-WebRequest -Uri "https://github.com/55gY/tdl-msgproce/releases/latest/download/tdl-msgproce_windows_amd64.exe" -OutFile "tdl-msgproce.exe"

# 创建目录并移动文件
New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\.tdl\extensions\tdl-msgproce"
Move-Item tdl-msgproce.exe "$env:USERPROFILE\.tdl\extensions\tdl-msgproce\tdl-msgproce.exe"
```

### 方法三：源码编译

**标准编译（本地）：**
```bash
git clone https://github.com/55gY/tdl-msgproce.git
cd tdl-msgproce

# 整理依赖
go mod tidy

# 编译
go build -ldflags="-s -w" -o tdl-msgproce

# 安装
mkdir -p ~/.tdl/extensions/tdl-msgproce
cp tdl-msgproce ~/.tdl/extensions/tdl-msgproce/
chmod +x ~/.tdl/extensions/tdl-msgproce/tdl-msgproce
```

**交叉编译（低内存服务器）：**

如果服务器内存不足（< 600MB），建议在本地编译后上传：

```bash
# Linux 目标
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o tdl-msgproce

# Windows 目标
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o tdl-msgproce.exe

# 上传到服务器
scp tdl-msgproce user@server:~/.tdl/extensions/tdl-msgproce/
```

**Windows PowerShell 交叉编译：**
```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags="-s -w" -o tdl-msgproce
```

## 📖 使用方法

### 1. 登录 Telegram

**使用二维码登录（推荐）：**
```bash
~/.tdl/tdl login -n default -T qr
```

**使用手机号登录：**
```bash
~/.tdl/tdl login -n default
```

### 2. 配置扩展

编辑配置文件：
```bash
nano ~/.tdl/extensions/data/msgproce/config.yaml
```

**必须配置的项目：**

```yaml
# 1. Bot Token（如需 Bot 功能）
bot:
  enabled: true
  token: "YOUR_BOT_TOKEN"  # 从 @BotFather 获取
  forward_target: 123456789  # 转发目标用户ID
  
# 2. 订阅 API（如需监听功能）
monitor:
  enabled: true
  subscription_api:
    api_key: "YOUR_API_KEY"
    add_url: "http://your-api.com:8080/api/subscription/add"
  
  # 3. 监听的频道ID
  channels:
    - 1234567890  # 使用 ~/.tdl/tdl chat ls 查看
    - 9876543210
```

**获取频道ID：**
```bash
~/.tdl/tdl chat ls -n default
```

### 3. 运行扩展

**方式一：控制台运行（前台，方便调试）**
```bash
~/.tdl/tdl -n default msgproce
```

**方式二：systemd 服务（后台，开机自启）**
```bash
# 使用安装脚本的菜单选项 5
./install.sh
# 或手动管理
systemctl start tdl-msgproce
systemctl enable tdl-msgproce
```

**方式三：使用管理脚本**
```bash
./install.sh  # 选择对应的菜单选项
```

### 4. 查看运行状态

**查看日志：**
```bash
# 文件日志
tail -f ~/.tdl/extensions/data/msgproce/log/latest.log

# systemd 日志
journalctl -u tdl-msgproce -f
```

**查看状态：**
```bash
# 使用管理脚本
./install.sh  # 选择菜单选项 9

# 或手动检查
systemctl status tdl-msgproce
ps aux | grep tdl
```

## 配置示例

### 完整配置示例

```yaml
# ==================== Bot 配置 ====================
bot:
  enabled: true
  token: "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
  allowed_users: [123456789, 987654321]  # 空列表=允许所有人
  forward_target: 1838605845
  forward_mode: "clone"

# ==================== 消息监听配置 ====================
monitor:
  enabled: true
  
  # API 配置
  subscription_api:
    api_key: "your_api_key_here"
    add_url: "http://api.example.com:8080/api/subscription/add"
  
  # 历史消息
  features:
    fetch_history_count: 100  # 获取最近100条历史消息
  
  # 监听频道（订阅链接）
  channels:
    - 2582776039  # 示例频道1
    - 1338209352  # 示例频道2
    - 1965523384  # 示例频道3
  
  # 白名单频道（跳过二次过滤）
  whitelist_channels:
    - 1313311705
  
  # 过滤规则
  filters:
    # 订阅链接过滤
    subs:
      - "https://"
      - "http://"
    
    # 节点链接过滤（全频道监听）
    ss:
      - "vmess://"
      - "vless://"
      - "ss://"
      - "ssr://"
      - "trojan://"
      - "hysteria://"
      - "hysteria2://"
      - "hy2://"
      - "tuic://"
      - "juicity://"
    
    # 二次内容过滤（订阅链接）
    content_filter:
      - "投稿"
      - "订阅"
      - "更新"
    
    # 黑名单
    link_blacklist:
      - "register"
      - "t.me"
      - ".jpg"
      - ".jpeg"
      - ".png"
      - ".gif"
      - ".webp"
      - ".bmp"
      - "go1.569521.xyz"
```

### 不同使用场景配置

**场景1：仅监听订阅链接**
```yaml
bot:
  enabled: false

monitor:
  enabled: true
  channels: [123456, 789012]
  filters:
    subs: ["https://", "http://"]
    ss: []  # 禁用节点监听
    content_filter: ["投稿", "订阅"]
```

**场景2：仅监听节点链接**
```yaml
bot:
  enabled: false

monitor:
  enabled: true
  channels: []  # 节点监听全频道，无需指定
  filters:
    subs: []  # 禁用订阅监听
    ss: ["vmess://", "vless://", "ss://", "trojan://"]
```

**场景3：Bot + 监听**
```yaml
bot:
  enabled: true
  token: "YOUR_BOT_TOKEN"
  forward_target: 123456789

monitor:
  enabled: true
  channels: [123456, 789012]
  filters:
    subs: ["https://"]
    ss: ["vmess://", "ss://"]
```

## 🛠️ 管理脚本

`install.sh` 提供完整的交互式管理功能：

```bash
./install.sh
```

### 主要功能

**安装相关：**
1. **完整安装** - 一键安装 tdl + msgproce + 配置引导
2. **仅安装 tdl** - 单独下载安装 tdl
3. **仅安装 msgproce** - 单独下载安装扩展

**运行控制：**
4. **控制台启动** - 前台运行，方便调试和查看输出
5. **安装 systemd 服务** - 后台运行，开机自启
6. **停止运行** - 停止所有相关进程
7. **重启服务** - 重启后台服务

**管理工具：**
8. **环境检测** - 检查 tdl、msgproce、配置文件等状态
9. **查看状态** - 显示运行状态、资源占用、路径信息
10. **查看日志** - 多种方式查看日志（最后50/100行、实时、完整）
11. **编辑配置** - 使用编辑器修改配置文件

**其他：**
12. **完全卸载** - 删除所有组件（可选保留数据）

### 安装路径

脚本使用的标准路径：
```
/root/.tdl/
├── tdl                              # tdl 主程序
├── data/                            # tdl 数据（登录信息）
└── extensions/
    ├── tdl-msgproce/               # 扩展可执行文件
    │   └── tdl-msgproce
    └── data/msgproce/              # 扩展数据
        ├── config.yaml             # 配置文件
        └── log/
            └── latest.log          # 运行日志
```

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

### 常见问题

**1. 扩展未找到**
```bash
# 检查文件是否存在
ls -la ~/.tdl/extensions/tdl-msgproce/tdl-msgproce

# 检查权限
chmod +x ~/.tdl/extensions/tdl-msgproce/tdl-msgproce

# 查看扩展列表
~/.tdl/tdl extension list
```

**2. 配置错误**
```bash
# 查看日志排查问题
tail -50 ~/.tdl/extensions/data/msgproce/log/latest.log

# 检查配置语法
cat ~/.tdl/extensions/data/msgproce/config.yaml
```

**3. Session 冲突**
确保没有多个 tdl 进程使用同一个 namespace：
```bash
ps aux | grep tdl
# 如有多个，停止其他进程
pkill -f "tdl.*msgproce"
```

**4. 无法连接到 Telegram**
```bash
# 检查网络连接
ping -c 3 telegram.org

# 检查登录状态
ls -la ~/.tdl/data/
```

**5. 自动下载失败**
手动下载：
- tdl: https://github.com/iyear/tdl/releases
- msgproce: https://github.com/55gY/tdl-msgproce/releases

```bash
# 手动安装示例
wget https://github.com/55gY/tdl-msgproce/releases/latest/download/tdl-msgproce_linux_amd64
chmod +x tdl-msgproce_linux_amd64
mkdir -p ~/.tdl/extensions/tdl-msgproce
mv tdl-msgproce_linux_amd64 ~/.tdl/extensions/tdl-msgproce/tdl-msgproce
```

**6. 编译内存不足**
在本地交叉编译后上传：
```bash
# 本地编译
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o tdl-msgproce

# 上传到服务器
scp tdl-msgproce user@server:~/.tdl/extensions/tdl-msgproce/
```

### 日志分析

**查看启动信息：**
```bash
tail -30 ~/.tdl/extensions/data/msgproce/log/latest.log | grep "启动"
```

**查看错误信息：**
```bash
grep -i "error\|fail\|错误" ~/.tdl/extensions/data/msgproce/log/latest.log
```

**实时监控：**
```bash
# 文件日志
tail -f ~/.tdl/extensions/data/msgproce/log/latest.log

# systemd 日志
journalctl -u tdl-msgproce -f
```

## 📦 版本发布

### 自动发布

**提交消息触发发布（推荐）：**

在 VSCode 中提交代码时，只需在提交消息中包含版本号：

```
v1.0.0
```

或

```
Release v1.0.1
```

GitHub Actions 会自动：
1. ✅ 检测提交消息中的版本号
2. ✅ 创建对应的 Git 标签
3. ✅ 编译 Linux 和 Windows 版本
4. ✅ 创建 GitHub Release
5. ✅ 上传二进制文件和配置文件

**支持的版本号格式：**
- `v1.0.0` ✅
- `1.0.0` ✅ (自动添加 v 前缀)
- `v2.1.3` ✅
- `v1.0.0-beta` ✅

详见：[`.github/RELEASE_GUIDE.md`](.github/RELEASE_GUIDE.md)

### 手动发布（传统方式）

```bash
# 创建标签
git tag v1.0.0

# 推送标签
git push origin v1.0.0
```

### 发布内容

每个 Release 包含：
- `tdl-msgproce_linux_amd64` - Linux 可执行文件
- `tdl-msgproce_windows_amd64.exe` - Windows 可执行文件
- `checksums.txt` - SHA256 校验和
- `config.yaml` - 配置文件模板
- `install.sh` - 安装脚本

### 下载最新版本

```bash
# Linux
wget https://github.com/55gY/tdl-msgproce/releases/latest/download/tdl-msgproce_linux_amd64

# Windows
Invoke-WebRequest -Uri "https://github.com/55gY/tdl-msgproce/releases/latest/download/tdl-msgproce_windows_amd64.exe" -OutFile "tdl-msgproce.exe"
```

## 📊 性能说明

### 资源占用
- **内存**：约 50-100MB（取决于监听频道数量）
- **CPU**：空闲时 < 1%，处理消息时 < 5%
- **磁盘**：日志文件自动轮转，配置约 10KB

### 编译优化
- 使用 `-ldflags="-s -w"` 去除调试信息
- 使用 `-trimpath` 去除路径信息
- 交叉编译支持多平台

### 性能建议
- 转发间隔建议 ≥ 300 秒，避免 API 限制
- 监听频道数量建议 < 50 个
- 定期清理日志文件

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

**贡献指南：**
1. Fork 项目
2. 创建功能分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 开源协议

MIT License - 详见 [LICENSE](LICENSE) 文件

## 🔗 相关链接

**主要项目：**
- **tdl**: https://github.com/iyear/tdl - 强大的 Telegram 下载器和工具集
- **本项目**: https://github.com/55gY/tdl-msgproce - 基于 tdl 的消息处理扩展

**历史项目：**
- **go-TelegramMessage**: https://github.com/55gY/go-TelegramMessage - 独立的消息监听器
- **go-bot**: https://github.com/55gY/go-bot - 独立的转发机器人  
- **ext-msgwait**: https://github.com/55gY/ext-msgwait - 早期混合版本（已弃用）

## ⭐ Star History

如果这个项目对你有帮助，请给一个 Star ⭐

[![Star History Chart](https://api.star-history.com/svg?repos=55gY/tdl-msgproce&type=Date)](https://star-history.com/#55gY/tdl-msgproce&Date)
