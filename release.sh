#!/bin/bash
# 快速发布脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}   tdl-msgproce 发布助手${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 检查是否有未提交的更改
if [[ -n $(git status -s) ]]; then
    echo -e "${RED}错误: 有未提交的更改${NC}"
    echo ""
    git status -s
    echo ""
    echo -e "${YELLOW}请先提交所有更改：${NC}"
    echo "  git add ."
    echo "  git commit -m \"your message\""
    exit 1
fi

# 获取当前分支
CURRENT_BRANCH=$(git branch --show-current)
echo -e "${BLUE}当前分支: ${CURRENT_BRANCH}${NC}"

# 获取最新标签
LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
echo -e "${BLUE}最新标签: ${LATEST_TAG}${NC}"
echo ""

# 解析版本号
if [[ $LATEST_TAG =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    MAJOR="${BASH_REMATCH[1]}"
    MINOR="${BASH_REMATCH[2]}"
    PATCH="${BASH_REMATCH[3]}"
else
    MAJOR=0
    MINOR=0
    PATCH=0
fi

# 计算新版本
NEXT_PATCH="v${MAJOR}.${MINOR}.$((PATCH + 1))"
NEXT_MINOR="v${MAJOR}.$((MINOR + 1)).0"
NEXT_MAJOR="v$((MAJOR + 1)).0.0"

echo -e "${YELLOW}选择版本类型：${NC}"
echo "  1) Patch (修复) - $NEXT_PATCH"
echo "  2) Minor (功能) - $NEXT_MINOR"
echo "  3) Major (重大) - $NEXT_MAJOR"
echo "  4) 自定义版本号"
echo "  0) 取消"
echo ""
read -p "请选择 [0-4]: " choice

case $choice in
    1)
        NEW_VERSION=$NEXT_PATCH
        ;;
    2)
        NEW_VERSION=$NEXT_MINOR
        ;;
    3)
        NEW_VERSION=$NEXT_MAJOR
        ;;
    4)
        read -p "输入版本号 (格式 v1.2.3): " NEW_VERSION
        if [[ ! $NEW_VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            echo -e "${RED}错误: 版本号格式不正确${NC}"
            exit 1
        fi
        ;;
    0)
        echo -e "${YELLOW}取消发布${NC}"
        exit 0
        ;;
    *)
        echo -e "${RED}无效选择${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${YELLOW}新版本: ${GREEN}${NEW_VERSION}${NC}"
echo ""
read -p "输入更新说明 (可选): " RELEASE_MESSAGE

if [ -z "$RELEASE_MESSAGE" ]; then
    RELEASE_MESSAGE="Release $NEW_VERSION"
fi

echo ""
echo -e "${YELLOW}准备发布：${NC}"
echo "  版本: $NEW_VERSION"
echo "  说明: $RELEASE_MESSAGE"
echo ""
read -p "确认发布? (y/n): " confirm

if [ "$confirm" != "y" ]; then
    echo -e "${YELLOW}取消发布${NC}"
    exit 0
fi

echo ""
echo -e "${BLUE}开始发布流程...${NC}"

# 创建标签
echo -e "${YELLOW}创建标签...${NC}"
git tag -a "$NEW_VERSION" -m "$RELEASE_MESSAGE"

# 推送到远程
echo -e "${YELLOW}推送到远程仓库...${NC}"
git push origin "$CURRENT_BRANCH"
git push origin "$NEW_VERSION"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✅ 发布成功！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}版本: ${GREEN}${NEW_VERSION}${NC}"
echo -e "${BLUE}GitHub Actions 正在构建...${NC}"
echo ""
echo -e "${YELLOW}查看构建状态：${NC}"
echo "  https://github.com/$(git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/')/actions"
echo ""
echo -e "${YELLOW}查看发布页面：${NC}"
echo "  https://github.com/$(git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/')/releases"
echo ""
