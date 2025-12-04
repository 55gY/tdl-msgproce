# tdl-msgproce 发布助手 (Windows PowerShell)

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Blue
Write-Host "   tdl-msgproce 发布助手" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Blue
Write-Host ""

# 检查是否有未提交的更改
$gitStatus = git status -s
if ($gitStatus) {
    Write-Host "错误: 有未提交的更改" -ForegroundColor Red
    Write-Host ""
    git status -s
    Write-Host ""
    Write-Host "请先提交所有更改：" -ForegroundColor Yellow
    Write-Host "  git add ."
    Write-Host "  git commit -m `"your message`""
    exit 1
}

# 获取当前分支
$currentBranch = git branch --show-current
Write-Host "当前分支: $currentBranch" -ForegroundColor Blue

# 获取最新标签
try {
    $latestTag = git describe --tags --abbrev=0 2>$null
} catch {
    $latestTag = "v0.0.0"
}
Write-Host "最新标签: $latestTag" -ForegroundColor Blue
Write-Host ""

# 解析版本号
if ($latestTag -match '^v(\d+)\.(\d+)\.(\d+)$') {
    $major = [int]$Matches[1]
    $minor = [int]$Matches[2]
    $patch = [int]$Matches[3]
} else {
    $major = 0
    $minor = 0
    $patch = 0
}

# 计算新版本
$nextPatch = "v$major.$minor.$($patch + 1)"
$nextMinor = "v$major.$($minor + 1).0"
$nextMajor = "v$($major + 1).0.0"

Write-Host "选择版本类型：" -ForegroundColor Yellow
Write-Host "  1) Patch (修复) - $nextPatch"
Write-Host "  2) Minor (功能) - $nextMinor"
Write-Host "  3) Major (重大) - $nextMajor"
Write-Host "  4) 自定义版本号"
Write-Host "  0) 取消"
Write-Host ""
$choice = Read-Host "请选择 [0-4]"

switch ($choice) {
    "1" { $newVersion = $nextPatch }
    "2" { $newVersion = $nextMinor }
    "3" { $newVersion = $nextMajor }
    "4" {
        $newVersion = Read-Host "输入版本号 (格式 v1.2.3)"
        if ($newVersion -notmatch '^v\d+\.\d+\.\d+$') {
            Write-Host "错误: 版本号格式不正确" -ForegroundColor Red
            exit 1
        }
    }
    "0" {
        Write-Host "取消发布" -ForegroundColor Yellow
        exit 0
    }
    default {
        Write-Host "无效选择" -ForegroundColor Red
        exit 1
    }
}

Write-Host ""
Write-Host "新版本: " -NoNewline -ForegroundColor Yellow
Write-Host $newVersion -ForegroundColor Green
Write-Host ""
$releaseMessage = Read-Host "输入更新说明 (可选)"

if ([string]::IsNullOrWhiteSpace($releaseMessage)) {
    $releaseMessage = "Release $newVersion"
}

Write-Host ""
Write-Host "准备发布：" -ForegroundColor Yellow
Write-Host "  版本: $newVersion"
Write-Host "  说明: $releaseMessage"
Write-Host ""
$confirm = Read-Host "确认发布? (y/n)"

if ($confirm -ne "y") {
    Write-Host "取消发布" -ForegroundColor Yellow
    exit 0
}

Write-Host ""
Write-Host "开始发布流程..." -ForegroundColor Blue

# 创建标签
Write-Host "创建标签..." -ForegroundColor Yellow
git tag -a $newVersion -m $releaseMessage

# 推送到远程
Write-Host "推送到远程仓库..." -ForegroundColor Yellow
git push origin $currentBranch
git push origin $newVersion

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "✅ 发布成功！" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "版本: " -NoNewline -ForegroundColor Blue
Write-Host $newVersion -ForegroundColor Green
Write-Host "GitHub Actions 正在构建..." -ForegroundColor Blue
Write-Host ""

# 获取仓库信息
$remoteUrl = git config --get remote.origin.url
if ($remoteUrl -match 'github\.com[:/](.+?)(?:\.git)?$') {
    $repo = $Matches[1]
    Write-Host "查看构建状态：" -ForegroundColor Yellow
    Write-Host "  https://github.com/$repo/actions"
    Write-Host ""
    Write-Host "查看发布页面：" -ForegroundColor Yellow
    Write-Host "  https://github.com/$repo/releases"
    Write-Host ""
}
