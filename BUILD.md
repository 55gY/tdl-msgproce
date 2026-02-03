# 自动编译和发布配置

本项目已配置 GitHub Actions 自动编译和发布流程。

## 📦 项目简介

`tdl-msgproce` 集成四大核心功能：
1. **消息监听** - 实时监听 Telegram 频道
2. **Bot 交互** - Telegram Bot 支持
3. **消息转发** - 自动转发频道消息
4. **定时签到** - 自动向机器人发送签到消息

## 📁 文件结构

```
.github/
├── workflows/
│   ├── release.yml       # 版本发布 workflow
│   └── build-test.yml    # 测试构建 workflow
└── RELEASE.md            # 发布流程详细说明

release.sh                # Linux/Mac 发布脚本
release.ps1               # Windows 发布脚本
.gitignore                # Git 忽略文件
```

## 🚀 快速发布

### 方法一：使用发布脚本（推荐）

**Linux/Mac:**
```bash
chmod +x release.sh
./release.sh
```

**Windows PowerShell:**
```powershell
.\release.ps1
```

脚本会引导你：
1. 选择版本类型（Patch/Minor/Major）
2. 输入更新说明
3. 自动创建标签并推送

### 方法二：手动发布

```bash
# 1. 提交所有更改
git add .
git commit -m "feat: 新功能描述"
git push

# 2. 创建版本标签
git tag -a v1.0.0 -m "Release v1.0.0"

# 3. 推送标签
git push origin v1.0.0
```

## 🏷️ 版本号规范

遵循 [语义化版本](https://semver.org/lang/zh-CN/) 规范：

- **v1.0.0** → **v1.0.1** - 修复 Bug (Patch)
- **v1.0.0** → **v1.1.0** - 新增功能 (Minor)
- **v1.0.0** → **v2.0.0** - 重大更新 (Major)

## 🔄 自动构建流程

### 触发条件

1. **发布构建** (`release.yml`)
   - 推送以 `v*.*.*` 格式的标签
   - 例如: `v1.0.0`, `v2.1.3`

2. **测试构建** (`build-test.yml`)
   - 推送到 main/master/dev 分支
   - 创建 Pull Request

### 构建产物

每次发布会自动生成：

- ✅ `tdl-msgproce_linux_amd64` - Linux 64位
- ✅ `tdl-msgproce_windows_amd64.exe` - Windows 64位
- ✅ `checksums.txt` - SHA256 校验和
- ✅ `config.yaml` - 配置文件模板
- ✅ `install.sh` - 安装脚本

### 发布位置

- GitHub Releases: `https://github.com/你的用户名/tdl-msgproce/releases`
- Actions 日志: `https://github.com/你的用户名/tdl-msgproce/actions`

## 📦 下载和安装

用户可以从 Releases 页面下载：

**Linux:**
```bash
wget https://github.com/你的用户名/tdl-msgproce/releases/download/v1.0.0/tdl-msgproce_linux_amd64
chmod +x tdl-msgproce_linux_amd64
mv tdl-msgproce_linux_amd64 tdl-msgproce
```

**Windows:**
```powershell
Invoke-WebRequest -Uri "https://github.com/你的用户名/tdl-msgproce/releases/download/v1.0.0/tdl-msgproce_windows_amd64.exe" -OutFile "tdl-msgproce.exe"
```

## 🔐 校验文件完整性

```bash
# 下载校验和文件
wget https://github.com/你的用户名/tdl-msgproce/releases/download/v1.0.0/checksums.txt

# 验证文件
sha256sum -c checksums.txt
```

## ⚙️ Workflow 配置说明

### release.yml

- **Go 版本**: 1.21
- **CGO**: 禁用 (`CGO_ENABLED=0`)
- **编译选项**: `-ldflags="-s -w"` (压缩二进制)
- **架构**: Linux/Windows AMD64

### build-test.yml

- 在每次提交和 PR 时运行
- 验证代码可以成功编译
- 不创建 Release，仅测试

## 🛠️ 本地测试

在推送前测试编译：

**Windows PowerShell:**
```powershell
# Windows 版本
go build -ldflags="-s -w" -o tdl-msgproce.exe

# Linux 版本（交叉编译）
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags="-s -w" -o tdl-msgproce
```

**Linux/Mac:**
```bash
# Linux 版本
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o tdl-msgproce

# Windows 版本（交叉编译）
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o tdl-msgproce.exe
```

## 📝 Commit 规范（推荐）

```
feat: 新功能
fix: Bug 修复
docs: 文档更新
style: 代码格式
refactor: 重构
perf: 性能优化
test: 测试
chore: 构建/工具变动
```

示例：
```bash
git commit -m "feat: 添加批量任务管理功能"
git commit -m "fix: 修复进度显示重复问题"
git commit -m "docs: 更新 README 安装说明"
```

## 🐛 常见问题

### 构建失败

1. 检查 Go 版本是否 >= 1.21
2. 确认 go.mod 依赖完整
3. 查看 Actions 日志详细错误

### 标签推送失败

```bash
# 如果标签已存在，先删除
git tag -d v1.0.0
git push origin :refs/tags/v1.0.0

# 重新创建
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### 权限问题

确保仓库设置中：
- Settings → Actions → General → Workflow permissions
- 选择 "Read and write permissions"

## 📚 更多信息

详见 `.github/RELEASE.md` 获取完整发布流程说明。
