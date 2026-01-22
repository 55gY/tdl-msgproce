#!/bin/bash
# tdl-msgproce 完整管理脚本
# 集成安装、配置、运行、监控等所有功能

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 路径配置
TDL_PATH="/root/.tdl/tdl"
MSGPROCE_PATH="/root/.tdl/extensions/tdl-msgproce/tdl-msgproce"
CONFIG_PATH="/root/.tdl/extensions/data/msgproce/config.yaml"
LOG_PATH="/root/.tdl/extensions/data/msgproce/log/latest.log"
EXTENSION_DIR="/root/.tdl/extensions/tdl-msgproce"
DATA_DIR="/root/.tdl/extensions/data/msgproce"
TDL_DATA_DIR="/root/.tdl/data"
SERVICE_FILE="/etc/systemd/system/tdl-msgproce.service"

# 调试模式配置（设置为 true 启用调试模式，false 禁用）
DEBUG=false
# 下载地址
TDL_RELEASE_URL="https://github.com/iyear/tdl/releases"
MSGPROCE_RELEASE_URL="https://github.com/55gY/tdl-msgproce/releases"

# 版本检测函数
get_latest_release() {
    local repo=$1
    curl -s "https://api.github.com/repos/${repo}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
}

# 显示横幅
show_banner() {
    clear
    echo -e "${BLUE}================================================${NC}"
    echo -e "${GREEN}      TDL-MSGPROCE 完整管理系统${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
}

# 检测系统架构
detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l)
            echo "armv7"
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

# 检测操作系统
detect_os() {
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        echo "linux"
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        echo "darwin"
    else
        echo "unknown"
    fi
}

# 下载 tdl
download_tdl() {
    echo -e "${YELLOW}开始下载 tdl...${NC}"
    
    local os=$(detect_os)
    local arch=$(detect_arch)
    
    if [ "$os" == "unknown" ] || [ "$arch" == "unknown" ]; then
        echo -e "${RED}不支持的系统架构: $os/$arch${NC}"
        echo -e "${YELLOW}请手动下载: ${TDL_RELEASE_URL}${NC}"
        return 1
    fi
    
    echo "检测到系统: $os/$arch"
    
    # 获取最新版本
    echo "获取最新版本信息..."
    local latest_version=$(get_latest_release "iyear/tdl")
    
    if [ -z "$latest_version" ]; then
        echo -e "${YELLOW}无法自动获取版本，使用默认版本...${NC}"
        latest_version="v0.20.0"
    fi
    
    echo "最新版本: $latest_version"
    
    # 构建下载链接（修正大小写）
    local os_proper=""
    local arch_proper=""
    
    # tdl 使用 Linux/Darwin 首字母大写
    if [ "$os" == "linux" ]; then
        os_proper="Linux"
    elif [ "$os" == "darwin" ]; then
        os_proper="Darwin"
    else
        os_proper="$os"
    fi
    
    # tdl 使用特定的架构命名
    case "$arch" in
        amd64)
            arch_proper="64bit"
            ;;
        arm64)
            arch_proper="arm64"
            ;;
        armv7)
            arch_proper="armv7"
            ;;
        *)
            arch_proper="$arch"
            ;;
    esac
    
    local download_url="https://github.com/iyear/tdl/releases/download/${latest_version}/tdl_${os_proper}_${arch_proper}.tar.gz"
    local tmp_file="/tmp/tdl_${latest_version}.tar.gz"
    
    echo "下载地址: $download_url"
    echo "下载中..."
    
    # 删除旧的临时文件
    rm -f "$tmp_file"
    
    if curl -L -o "$tmp_file" "$download_url"; then
        # 验证下载的文件是否为 gzip 格式
        if file "$tmp_file" | grep -q "gzip"; then
            echo "解压中..."
            mkdir -p /root/.tdl
            tar -xzf "$tmp_file" -C /tmp/
            
            if [ -f "/tmp/tdl" ]; then
                mv /tmp/tdl "$TDL_PATH"
                chmod +x "$TDL_PATH"
                rm -f "$tmp_file"
                echo -e "${GREEN}✅ tdl 下载成功${NC}"
                $TDL_PATH version
                return 0
            else
                echo -e "${RED}解压后未找到 tdl 文件${NC}"
                rm -f "$tmp_file"
                return 1
            fi
        else
            echo -e "${RED}下载的文件格式不正确（可能是 404 页面）${NC}"
            echo "文件类型: $(file "$tmp_file")"
            rm -f "$tmp_file"
            return 1
        fi
    else
        echo -e "${RED}下载失败${NC}"
        echo -e "${YELLOW}请手动下载并安装: ${TDL_RELEASE_URL}${NC}"
        return 1
    fi
}

# 下载 msgproce
download_msgproce() {
    echo -e "${YELLOW}开始下载 tdl-msgproce...${NC}"
    
    local os=$(detect_os)
    local arch=$(detect_arch)
    
    if [ "$os" == "unknown" ] || [ "$arch" == "unknown" ]; then
        echo -e "${RED}不支持的系统架构: $os/$arch${NC}"
        echo -e "${YELLOW}请手动下载: ${MSGPROCE_RELEASE_URL}${NC}"
        return 1
    fi
    
    # 只支持 Linux 和 Darwin 的 amd64
    if [ "$os" != "linux" ] && [ "$os" != "darwin" ]; then
        echo -e "${RED}当前只支持 Linux 和 macOS${NC}"
        return 1
    fi
    
    if [ "$arch" != "amd64" ]; then
        echo -e "${RED}当前只支持 amd64 架构${NC}"
        echo -e "${YELLOW}请从源码编译或等待其他架构支持${NC}"
        return 1
    fi
    
    echo "检测到系统: $os/$arch"
    
    # 获取最新版本
    echo "获取最新版本信息..."
    local latest_version=$(get_latest_release "55gY/tdl-msgproce")
    
    if [ -z "$latest_version" ]; then
        echo -e "${YELLOW}无法自动获取版本${NC}"
        echo -e "${YELLOW}请手动下载: ${MSGPROCE_RELEASE_URL}${NC}"
        return 1
    fi
    
    echo "最新版本: $latest_version"
    
    # 构建下载链接（匹配 workflow 命名规则）
    local download_url="https://github.com/55gY/tdl-msgproce/releases/download/${latest_version}/tdl-msgproce_${os}_amd64"
    local tmp_file="/tmp/tdl-msgproce_${os}_amd64"
    
    echo "下载地址: $download_url"
    echo "下载到临时目录: $tmp_file"
    echo "下载中..."
    
    # 删除旧的临时文件
    rm -f "$tmp_file"
    
    if curl -L -o "$tmp_file" "$download_url"; then
        # 验证下载的文件是否为可执行文件
        if file "$tmp_file" | grep -qE "(executable|ELF)"; then
            chmod +x "$tmp_file"
            echo -e "${GREEN}✅ tdl-msgproce 下载成功（临时文件）${NC}"
            return 0
        else
            echo -e "${RED}下载的文件格式不正确（可能是 404 页面）${NC}"
            echo "文件类型: $(file "$tmp_file")"
            rm -f "$tmp_file"
            return 1
        fi
    else
        echo -e "${RED}下载失败${NC}"
        echo -e "${YELLOW}请手动下载并安装: ${MSGPROCE_RELEASE_URL}${NC}"
        rm -f "$tmp_file"
        return 1
    fi
}

# 检查 tdl 是否安装
check_tdl() {
    if [ -f "$TDL_PATH" ]; then
        return 0
    fi
    return 1
}

# 检查 msgproce 是否安装
check_msgproce() {
    if [ -f "$MSGPROCE_PATH" ]; then
        return 0
    fi
    return 1
}

# 检查 tdl 是否已登录
check_tdl_login() {
    if [ -d "$TDL_DATA_DIR" ] && [ -n "$(ls -A $TDL_DATA_DIR 2>/dev/null)" ]; then
        return 0
    fi
    return 1
}

# 检查配置文件
check_config() {
    if [ -f "$CONFIG_PATH" ]; then
        # 检查是否包含默认值（未修改）
        if grep -q "YOUR_BOT_TOKEN" "$CONFIG_PATH" || grep -q "YOUR_API_HOST" "$CONFIG_PATH"; then
            return 1  # 配置未修改
        fi
        return 0  # 配置已修改
    fi
    return 2  # 配置不存在
}

# 环境完整检测
check_environment() {
    echo -e "${CYAN}🔍 检测系统环境...${NC}"
    echo ""
    
    local all_ok=true
    
    # 1. 检查 tdl
    echo -n "检查 tdl: "
    if check_tdl; then
        local version=$($TDL_PATH version 2>/dev/null | head -n 1 || echo "未知")
        echo -e "${GREEN}✅ 已安装${NC} ($version)"
    else
        echo -e "${RED}❌ 未安装${NC}"
        all_ok=false
    fi
    
    # 2. 检查 tdl 登录状态
    echo -n "检查 tdl 登录: "
    if check_tdl_login; then
        echo -e "${GREEN}✅ 已登录${NC}"
    else
        echo -e "${YELLOW}⚠️  未登录${NC}"
        all_ok=false
    fi
    
    # 3. 检查 msgproce
    echo -n "检查 msgproce: "
    if check_msgproce; then
        local size=$(du -h "$MSGPROCE_PATH" 2>/dev/null | cut -f1)
        echo -e "${GREEN}✅ 已安装${NC} ($size)"
    else
        echo -e "${RED}❌ 未安装${NC}"
        all_ok=false
    fi
    
    # 4. 检查配置文件
    echo -n "检查配置文件: "
    check_config
    local config_status=$?
    if [ $config_status -eq 0 ]; then
        echo -e "${GREEN}✅ 已配置${NC}"
    elif [ $config_status -eq 1 ]; then
        echo -e "${YELLOW}⚠️  使用默认值（需修改）${NC}"
        all_ok=false
    else
        echo -e "${RED}❌ 不存在${NC}"
        all_ok=false
    fi
    
    # 5. 检查运行状态
    echo -n "运行状态: "
    if pgrep -f "tdl.*msgproce" > /dev/null; then
        echo -e "${GREEN}✅ 运行中${NC}"
    else
        echo -e "${YELLOW}⚠️  未运行${NC}"
    fi
    
    # 6. 检查 systemd 服务
    echo -n "systemd 服务: "
    if [ -f "$SERVICE_FILE" ]; then
        if systemctl is-active --quiet tdl-msgproce; then
            echo -e "${GREEN}✅ 运行中${NC}"
        else
            echo -e "${YELLOW}⚠️  已安装未运行${NC}"
        fi
    else
        echo -e "${YELLOW}⚠️  未安装${NC}"
    fi
    
    echo ""
    
    if [ "$all_ok" = true ]; then
        echo -e "${GREEN}✅ 环境检测通过，可以正常运行${NC}"
        return 0
    else
        echo -e "${YELLOW}⚠️  环境不完整，请完成安装和配置${NC}"
        return 1
    fi
}

# 安装 tdl
install_tdl() {
    if check_tdl; then
        echo -e "${YELLOW}tdl 已安装，准备覆盖安装...${NC}"
        echo -e "${YELLOW}停止运行中的服务...${NC}"
        stop_service
        echo ""
    fi
    
    download_tdl
    
    if check_tdl; then
        echo ""
        echo -e "${GREEN}✅ tdl 安装成功${NC}"
        echo ""
        
        # 检测登录状态
        if check_tdl_login; then
            echo -e "${GREEN}✅ 检测到已登录${NC}"
        else
            echo -e "${YELLOW}检测到未登录${NC}"
            echo -e "${YELLOW}下一步：登录 Telegram 账号${NC}"
            echo "运行: $TDL_PATH login -n default -T qr"
            echo ""
            echo -n "是否现在登录? (y/n): "
            read -r answer
            if [ "$answer" = "y" ]; then
                $TDL_PATH login -n default -T qr
            fi
        fi
        return 0
    else
        echo -e "${RED}tdl 安装失败${NC}"
        return 1
    fi
}

# 创建默认配置文件
create_default_config() {
    if [ -f "$CONFIG_PATH" ]; then
        echo -e "${YELLOW}配置文件已存在，保留现有配置${NC}"
        return 0
    fi
    
    echo "创建默认配置文件..."
    mkdir -p "$DATA_DIR"
    
    # 从 GitHub 下载配置文件
    local config_url="https://raw.githubusercontent.com/55gY/tdl-msgproce/main/config.yaml"
    
    echo "从 GitHub 下载配置模板..."
    if curl -sSL -o "$CONFIG_PATH" "$config_url"; then
        echo -e "${GREEN}✅ 配置文件下载成功${NC}"
        echo -e "${RED}⚠️  重要：请立即编辑配置文件！${NC}"
        echo "   位置: $CONFIG_PATH"
        return 0
    else
        echo -e "${YELLOW}⚠️  下载失败，使用内置模板${NC}"
        
        # 备用方案：使用内置模板
        cat > "$CONFIG_PATH" << 'EOF'
# tdl-msgproce 配置文件

# Telegram Bot 配置
bot:
  token: "YOUR_BOT_TOKEN"  # 从 @BotFather 获取
  forward_target: 0        # 转发目标用户ID（0=发送者本人）

# 自动添加到订阅 API 配置
subscription_api:
  api_key: "YOUR_API_KEY"             # API 密钥
  add_url: "YOUR_API_ADD_URL"         # 完整的添加订阅 URL，例如: http://api.example.com:8080/api/subscription/add

# 获取频道历史信息的功能开关
features:
  fetch_history_enabled: true  # 是否在启动时获取历史消息

# 监听配置
monitor:
  # 要监听的频道ID列表
  channels:
    # - 1234567890  # 示例频道ID
    # 使用 tdl chat ls 查看频道ID

  # 白名单频道 - 这些频道不经过二次内容过滤
  whitelist_channels:
    # - 1234567890

# 过滤配置
filters:
  # 关键词列表 - 消息必须包含这些关键词之一
  keywords:
    - "https://"
    - "http://"

  # 内容过滤 - 二次过滤，消息内容必须包含这些词之一
  content_filter:
    - "投稿"
    - "订阅"

  # 链接黑名单 - 包含这些关键字的链接不显示
  link_blacklist:
    - "register"
    - "t.me"
    - ".jpg"
    - ".jpeg"
    - ".png"
    - ".gif"
    - ".webp"
    - ".bmp"
EOF
        
        echo -e "${GREEN}✅ 使用内置配置文件${NC}"
        echo -e "${RED}⚠️  重要：请立即编辑配置文件！${NC}"
        echo "   位置: $CONFIG_PATH"
        return 0
    fi
}

# 安装 msgproce
install_msgproce() {
    if ! check_tdl; then
        echo -e "${RED}错误: 请先安装 tdl${NC}"
        return 1
    fi
    
    if check_msgproce; then
        echo -e "${YELLOW}msgproce 已安装，准备覆盖安装...${NC}"
        echo -e "${YELLOW}停止运行中的服务...${NC}"
        stop_service
        echo ""
    fi
    
    local os=$(detect_os)
    local tmp_file="/tmp/tdl-msgproce_${os}_amd64"
    
    # 尝试下载到 /tmp
    if ! download_msgproce; then
        # 下载失败，检查当前目录是否有编译好的文件
        echo ""
        echo -e "${YELLOW}尝试使用当前目录的文件...${NC}"
        
        if [ -f "./tdl-msgproce" ]; then
            echo "发现本地文件，复制到临时目录..."
            cp ./tdl-msgproce "$tmp_file"
            chmod +x "$tmp_file"
            echo -e "${GREEN}✅ 本地文件准备完成${NC}"
        else
            echo -e "${RED}当前目录也没有 tdl-msgproce 文件${NC}"
            echo ""
            echo -e "${YELLOW}解决方案:${NC}"
            echo "1. 从 ${MSGPROCE_RELEASE_URL} 手动下载"
            echo "2. 或在本地编译后上传到此目录"
            echo "3. 然后重新运行此脚本"
            return 1
        fi
    fi
    
    # 验证临时文件存在
    if [ ! -f "$tmp_file" ]; then
        echo -e "${RED}错误: 临时文件不存在: $tmp_file${NC}"
        return 1
    fi
    
    # 创建数据目录
    mkdir -p "$DATA_DIR/log"
    
    # 创建配置文件
    create_default_config
    
    # 重命名为正确的扩展名称（注册前）
    local tmp_renamed="/tmp/tdl-msgproce"
    echo ""
    echo -e "${YELLOW}准备注册文件...${NC}"
    cp "$tmp_file" "$tmp_renamed"
    chmod +x "$tmp_renamed"
    echo "临时文件: $tmp_renamed"
    
    # 从临时目录注册扩展到 tdl（tdl 会自动复制文件）
    echo -e "${YELLOW}注册扩展到 tdl...${NC}"
    if $TDL_PATH extension install --force "$tmp_renamed"; then
        echo -e "${GREEN}✅ 扩展注册成功（tdl 已自动复制文件）${NC}"
        
        # 清理临时文件
        rm -f "$tmp_file" "$tmp_renamed"
        echo -e "${GREEN}✅ 临时文件已清理${NC}"
    else
        echo -e "${RED}⚠️  扩展注册失败${NC}"
        echo -e "${YELLOW}可手动执行:${NC}"
        echo "   $TDL_PATH extension install --force $tmp_renamed"
        return 1
    fi
    
    echo -e "${GREEN}✅ msgproce 安装完成${NC}"
    echo ""
    
    # 检查登录状态并决定是否启动服务
    if check_tdl_login; then
        echo -e "${GREEN}✅ 检测到 tdl 已登录${NC}"
        echo ""
        echo -e "${YELLOW}自动安装 systemd 服务并启动...${NC}"
        install_service
    else
        echo -e "${YELLOW}⚠️  检测到 tdl 未登录${NC}"
        echo -e "${YELLOW}请先登录 Telegram 账号才能启动服务${NC}"
        echo ""
        echo -n "是否现在登录? (y/n): "
        read -r answer
        if [ "$answer" = "y" ]; then
            $TDL_PATH login -n default -T qr
            echo ""
            echo -e "${YELLOW}登录完成后，可使用以下命令启动服务:${NC}"
            echo "  控制台: $TDL_PATH -n default msgproce"
            echo "  或重新运行此脚本选择安装 systemd 服务"
        else
            echo -e "${YELLOW}跳过登录${NC}"
            echo -e "${YELLOW}稍后请手动登录: $TDL_PATH login -n default -T qr${NC}"
        fi
    fi
    
    return 0
}

# 完整安装（tdl + msgproce + 配置）
full_install() {
    echo -e "${CYAN}🚀 开始完整安装...${NC}"
    echo ""
    
    # 1. 安装 tdl
    echo -e "${BLUE}=== 第 1 步：安装 tdl ===${NC}"
    if ! install_tdl; then
        echo -e "${RED}tdl 安装失败，中止安装${NC}"
        return 1
    fi
    echo ""
    
    # 2. 检查登录
    echo -e "${BLUE}=== 第 2 步：检查登录状态 ===${NC}"
    if ! check_tdl_login; then
        echo -e "${YELLOW}检测到未登录${NC}"
        echo -n "是否现在登录? (y/n): "
        read -r answer
        if [ "$answer" = "y" ]; then
            $TDL_PATH login -n default -T qr
        else
            echo -e "${YELLOW}跳过登录，稍后请手动运行: $TDL_PATH login -n default -T qr${NC}"
        fi
    else
        echo -e "${GREEN}✅ 已登录${NC}"
    fi
    echo ""
    
    # 3. 安装 msgproce
    echo -e "${BLUE}=== 第 3 步：安装 msgproce 扩展 ===${NC}"
    if ! install_msgproce; then
        echo -e "${RED}msgproce 安装失败${NC}"
        return 1
    fi
    echo ""
    
    # 4. 编辑配置
    echo -e "${BLUE}=== 第 4 步：配置扩展 ===${NC}"
    check_config
    local config_status=$?
    
    if [ $config_status -ne 0 ]; then
        echo -e "${YELLOW}需要配置${NC}"
        echo ""
        echo -e "${YELLOW}必须配置的项目:${NC}"
        echo "  1. bot.token - Bot Token (从 @BotFather 获取)"
        echo "  2. subscription_api.host - API 服务器地址"
        echo "  3. subscription_api.api_key - API 密钥"
        echo "  4. monitor.channels - 要监听的频道ID列表"
        echo ""
        echo -e "${YELLOW}获取频道ID:${NC}"
        echo "  $TDL_PATH chat ls -n default"
        echo ""
        echo -n "是否现在编辑配置? (y/n): "
        read -r answer
        if [ "$answer" = "y" ]; then
            ${EDITOR:-nano} "$CONFIG_PATH"
        else
            echo -e "${RED}⚠️  配置未完成，启动前必须完成配置！${NC}"
        fi
    fi
    echo ""
    
    # 5. 询问启动方式
    echo -e "${BLUE}=== 第 5 步：启动服务 ===${NC}"
    echo -e "${YELLOW}选择启动方式:${NC}"
    echo "1) 控制台启动（前台运行，方便调试）"
    echo "2) 安装 systemd 服务（后台运行，开机自启）"
    echo "3) 稍后手动启动"
    echo ""
    echo -n "请选择 [1-3]: "
    read -r choice
    
    case $choice in
        1)
            start_console
            ;;
        2)
            install_service
            ;;
        3)
            echo -e "${YELLOW}跳过启动${NC}"
            echo ""
            echo -e "${YELLOW}手动启动命令:${NC}"
            echo "  控制台: $TDL_PATH -n default msgproce"
            echo "  或安装服务: 选择主菜单选项"
            ;;
        *)
            echo -e "${YELLOW}无效选择，跳过启动${NC}"
            ;;
    esac
    
    echo ""
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}✅ 安装完成！${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# 控制台启动（前台）
start_console() {
    if ! check_msgproce; then
        echo -e "${RED}错误: 扩展未安装${NC}"
        return 1
    fi
    
    if ! check_tdl_login; then
        echo -e "${RED}错误: tdl 未登录${NC}"
        echo "请先运行: $TDL_PATH login -n default -T qr"
        return 1
    fi
    
    check_config
    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 配置文件未完成${NC}"
        echo "请先编辑: $CONFIG_PATH"
        return 1
    fi
    
    echo -e "${YELLOW}控制台模式启动（按 Ctrl+C 停止）${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
    
    # 构建命令参数
    local debug_flag=""
    if [ "$DEBUG" = "true" ]; then
        debug_flag="--debug"
        echo -e "${CYAN}调试模式已启用${NC}"
    fi
    
    # 直接运行，输出到终端
    exec $TDL_PATH -n "default" --delay 5s --limit 10 $debug_flag msgproce
}

# 安装 systemd 服务
install_service() {
    if ! check_msgproce; then
        echo -e "${RED}错误: 扩展未安装${NC}"
        return 1
    fi
    
    # >>>>>>>>> 新增步骤：安装前先停止所有旧进程 <<<<<<<<<<<
    echo -e "${YELLOW}安装服务前，确保所有旧进程已停止...${NC}"
    stop_service
    echo ""
    
    echo -e "${YELLOW}创建 systemd 服务...${NC}"
    
    # 根据 DEBUG 变量构建命令
    local debug_flag=""
    if [ "$DEBUG" = "true" ]; then
        debug_flag="--debug"
        echo -e "${CYAN}调试模式已启用${NC}"
    fi
    
    # 创建服务文件
    cat > "$SERVICE_FILE" << EOF
[Unit]
Description=TDL Message Processor Extension
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/.tdl
ExecStart=$TDL_PATH -n "default" --delay 5s --limit 10 $debug_flag msgproce
# 停止服务时，确保相关进程被终止
ExecStop=/usr/bin/pkill -f "tdl.*msgproce"
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
# 确保 systemd 清理所有子进程
KillMode=control-group

[Install]
WantedBy=multi-user.target
EOF

    # 重载systemd
    systemctl daemon-reload
    
    # 启用服务
    systemctl enable tdl-msgproce
    
    echo -e "${GREEN}✅ systemd 服务已安装并启用${NC}"
    
    # 立即启动服务
    echo -e "${YELLOW}启动服务...${NC}"
    systemctl start tdl-msgproce
    sleep 2
    
    if systemctl is-active --quiet tdl-msgproce; then
        echo -e "${GREEN}✅ 服务已启动${NC}"
        systemctl status tdl-msgproce --no-pager -l
    else
        echo -e "${RED}服务启动失败${NC}"
        systemctl status tdl-msgproce --no-pager -l
        return 1
    fi
    
    echo ""
    echo -e "${BLUE}服务管理命令:${NC}"
    echo "  启动: systemctl start tdl-msgproce"
    echo "  停止: systemctl stop tdl-msgproce"
    echo "  重启: systemctl restart tdl-msgproce"
    echo "  状态: systemctl status tdl-msgproce"
    echo "  日志: journalctl -u tdl-msgproce -f"
    
    return 0
}

# 停止运行
stop_service() {
    echo -e "${YELLOW}停止 tdl-msgproce...${NC}"
    
    local stopped_systemd=false
    local stopped_process=false

    # 步骤 1: 停止 systemd 服务
    if systemctl is-active --quiet tdl-msgproce 2>/dev/null; then
        echo "正在停止 systemd 服务..."
        systemctl stop tdl-msgproce
        sleep 1
        if ! systemctl is-active --quiet tdl-msgproce 2>/dev/null; then
            echo -e "${GREEN}✅ systemd 服务已停止${NC}"
            stopped_systemd=true
        else
            echo -e "${RED}⚠️ systemd 服务停止失败${NC}"
        fi
    fi

    # 步骤 2: 查找并终止所有相关进程（作为双重保障）
    if pgrep -f "tdl.*msgproce" > /dev/null; then
        echo "正在终止相关进程..."
        # 使用 pkill 友好地终止
        pkill -f "tdl.*msgproce"
        sleep 1

        # 检查是否仍在运行，如果还在则强制终止
        if pgrep -f "tdl.*msgproce" > /dev/null; then
            echo -e "${YELLOW}常规停止失败，尝试强制终止...${NC}"
            pkill -9 -f "tdl.*msgproce"
            sleep 1
        fi

        if ! pgrep -f "tdl.*msgproce" > /dev/null; then
            echo -e "${GREEN}✅ 所有相关进程已终止${NC}"
            stopped_process=true
        else
            echo -e "${RED}❌ 进程终止失败${NC}"
        fi
    else
        stopped_process=true # 如果本来就没有进程，也视为成功
    fi

    if [ "$stopped_systemd" = true ] || [ "$stopped_process" = true ]; then
        if [ "$stopped_systemd" = false ] && [ "$stopped_process" = false ]; then
             echo -e "${YELLOW}未发现运行中的服务或进程${NC}"
        fi
    fi
}

# 重启服务
restart_service() {
    echo -e "${YELLOW}重启 tdl-msgproce...${NC}"
    
    # 调用增强的停止函数
    stop_service
    sleep 1
    
    echo "正在启动服务..."
    if systemctl is-enabled --quiet tdl-msgproce 2>/dev/null; then
        systemctl start tdl-msgproce
        sleep 2
        if systemctl is-active --quiet tdl-msgproce; then
            echo -e "${GREEN}✅ systemd 服务已重启${NC}"
            systemctl status tdl-msgproce --no-pager -l
        else
            echo -e "${RED}❌ 重启失败，服务未能启动${NC}"
            systemctl status tdl-msgproce --no-pager -l
        fi
    else
        echo -e "${YELLOW}⚠️ systemd 服务未安装，请使用“控制台启动”或“安装 systemd 服务”${NC}"
    fi
}

# 完全卸载
uninstall_all() {
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}警告：这将删除所有组件和数据${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "将要删除:"
    echo "  - tdl ($TDL_PATH)"
    echo "  - msgproce 扩展 ($MSGPROCE_PATH)"
    echo "  - 配置和日志 ($DATA_DIR)"
    echo "  - systemd 服务"
    echo ""
    echo -n "确认删除? 输入 'yes' 继续: "
    read -r answer
    
    if [ "$answer" != "yes" ]; then
        echo -e "${YELLOW}取消卸载${NC}"
        return 0
    fi
    
    echo ""
    echo -e "${YELLOW}开始卸载...${NC}"
    
    # 1. 停止服务
    stop_service
    
    # 2. 删除 systemd 服务
    if [ -f "$SERVICE_FILE" ]; then
        systemctl disable tdl-msgproce 2>/dev/null
        rm -f "$SERVICE_FILE"
        systemctl daemon-reload
        echo -e "${GREEN}✅ systemd 服务已删除${NC}"
    fi
    
    # 3. 删除扩展
    if [ -d "$EXTENSION_DIR" ]; then
        rm -rf "$EXTENSION_DIR"
        echo -e "${GREEN}✅ 扩展目录已删除${NC}"
    fi
    
    # 4. 询问是否删除数据
    echo ""
    echo -n "是否删除配置和日志? (y/n): "
    read -r delete_data
    
    if [ "$delete_data" = "y" ]; then
        if [ -d "$DATA_DIR" ]; then
            rm -rf "$DATA_DIR"
            echo -e "${GREEN}✅ 数据目录已删除${NC}"
        fi
    else
        echo -e "${YELLOW}保留配置和日志${NC}"
    fi
    
    # 5. 询问是否删除 tdl
    echo ""
    echo -n "是否删除 tdl? (y/n): "
    read -r delete_tdl
    
    if [ "$delete_tdl" = "y" ]; then
        if [ -f "$TDL_PATH" ]; then
            rm -f "$TDL_PATH"
            echo -e "${GREEN}✅ tdl 已删除${NC}"
        fi
        
        echo -n "是否删除 tdl 数据目录 (包含登录信息)? (y/n): "
        read -r delete_tdl_data
        
        if [ "$delete_tdl_data" = "y" ]; then
            if [ -d "/root/.tdl" ]; then
                rm -rf /root/.tdl
                echo -e "${GREEN}✅ tdl 数据目录已删除${NC}"
            fi
        fi
    else
        echo -e "${YELLOW}保留 tdl${NC}"
    fi
    
    echo ""
    echo -e "${GREEN}✅ 卸载完成${NC}"
}

# 查看状态
show_status() {
    echo ""
    echo -e "${BLUE}=== 系统状态 ===${NC}"
    echo ""
    
    # tdl
    echo -n "tdl: "
    if check_tdl; then
        local version=$($TDL_PATH version 2>/dev/null | head -n 1 || echo "未知")
        echo -e "${GREEN}✅ 已安装${NC} - $version"
    else
        echo -e "${RED}❌ 未安装${NC}"
    fi
    
    # tdl 登录
    echo -n "tdl 登录: "
    if check_tdl_login; then
        echo -e "${GREEN}✅ 已登录${NC}"
    else
        echo -e "${RED}❌ 未登录${NC}"
    fi
    
    # msgproce
    echo -n "msgproce: "
    if check_msgproce; then
        local size=$(du -h "$MSGPROCE_PATH" 2>/dev/null | cut -f1)
        echo -e "${GREEN}✅ 已安装${NC} - $size"
    else
        echo -e "${RED}❌ 未安装${NC}"
    fi
    
    # 配置
    echo -n "配置文件: "
    check_config
    local config_status=$?
    if [ $config_status -eq 0 ]; then
        echo -e "${GREEN}✅ 已配置${NC}"
    elif [ $config_status -eq 1 ]; then
        echo -e "${YELLOW}⚠️  需修改${NC}"
    else
        echo -e "${RED}❌ 不存在${NC}"
    fi
    
    # 运行状态
    echo -n "运行状态: "
    if pgrep -f "tdl.*msgproce" > /dev/null; then
        local pid=$(pgrep -f "tdl.*msgproce")
        echo -e "${GREEN}✅ 运行中${NC} (PID: $pid)"
        
        if command -v ps &> /dev/null; then
            local cpu=$(ps -p "$pid" -o %cpu --no-headers 2>/dev/null | xargs)
            local mem=$(ps -p "$pid" -o %mem --no-headers 2>/dev/null | xargs)
            echo "  CPU: ${cpu}% | 内存: ${mem}%"
        fi
    else
        echo -e "${RED}❌ 未运行${NC}"
    fi
    
    # systemd 服务
    echo -n "systemd 服务: "
    if [ -f "$SERVICE_FILE" ]; then
        if systemctl is-active --quiet tdl-msgproce; then
            echo -e "${GREEN}✅ 运行中${NC}"
        else
            echo -e "${YELLOW}⚠️  已安装未运行${NC}"
        fi
    else
        echo -e "${YELLOW}⚠️  未安装${NC}"
    fi
    
    echo ""
    echo -e "${BLUE}=== 路径信息 ===${NC}"
    echo "tdl: $TDL_PATH"
    echo "扩展: $MSGPROCE_PATH"
    echo "配置: $CONFIG_PATH"
    echo "日志: $LOG_PATH"
    echo ""
}

# 查看日志
view_logs() {
    if [ ! -f "$LOG_PATH" ]; then
        echo -e "${RED}日志文件不存在${NC}"
        echo ""
        echo "如果使用 systemd 服务，查看日志:"
        echo "  journalctl -u tdl-msgproce -f"
        return 1
    fi
    
    echo -e "${YELLOW}选择查看方式:${NC}"
    echo "1) 最后 50 行"
    echo "2) 最后 100 行"
    echo "3) 实时查看 (tail -f)"
    echo "4) 完整查看 (less)"
    echo "5) systemd 日志"
    echo ""
    echo -n "请选择 [1-5]: "
    read -r choice
    
    case $choice in
        1)
            tail -n 50 "$LOG_PATH"
            ;;
        2)
            tail -n 100 "$LOG_PATH"
            ;;
        3)
            echo -e "${YELLOW}按 Ctrl+C 退出${NC}"
            sleep 1
            tail -f "$LOG_PATH"
            ;;
        4)
            less "$LOG_PATH"
            ;;
        5)
            if systemctl list-units | grep -q tdl-msgproce; then
                echo -e "${YELLOW}按 Ctrl+C 退出${NC}"
                sleep 1
                journalctl -u tdl-msgproce -f
            else
                echo -e "${RED}systemd 服务未运行${NC}"
            fi
            ;;
        *)
            echo -e "${RED}无效选择${NC}"
            ;;
    esac
}

# 编辑配置
edit_config() {
    if [ ! -f "$CONFIG_PATH" ]; then
        echo -e "${RED}配置文件不存在${NC}"
        echo -n "是否创建默认配置? (y/n): "
        read -r answer
        if [ "$answer" = "y" ]; then
            create_default_config
        else
            return 1
        fi
    fi
    
    ${EDITOR:-nano} "$CONFIG_PATH"
    
    echo ""
    echo -n "配置已修改，是否重启服务? (y/n): "
    read -r answer
    if [ "$answer" = "y" ]; then
        restart_service
    fi
}

# 主菜单
main_menu() {
    while true; do
        show_banner
        
        # 显示快速状态
        echo -n "tdl: "
        if check_tdl; then
            echo -ne "${GREEN}✅${NC} "
        else
            echo -ne "${RED}❌${NC} "
        fi
        
        echo -n "| msgproce: "
        if check_msgproce; then
            echo -ne "${GREEN}✅${NC} "
        else
            echo -ne "${RED}❌${NC} "
        fi
        
        echo -n "| 运行: "
        if pgrep -f "tdl.*msgproce" > /dev/null; then
            echo -e "${GREEN}✅${NC}"
        else
            echo -e "${RED}❌${NC}"
        fi
        
        echo ""
        echo -e "${BLUE}请选择操作:${NC}"
        echo ""
        echo -e "  ${GREEN}安装相关:${NC}"
        echo "    1) 完整安装 (推荐首次使用)"
        echo "    2) 仅安装 tdl"
        echo "    3) 仅安装 msgproce 扩展"
        echo ""
        echo -e "  ${CYAN}运行控制:${NC}"
        echo "    4) 控制台启动 (前台)"
        echo "    5) 安装 systemd 服务"
        echo "    6) 停止运行"
        echo "    7) 重启服务"
        echo ""
        echo -e "  ${YELLOW}管理工具:${NC}"
        echo "    8) 环境检测"
        echo "    9) 查看状态"
        echo "   10) 查看日志"
        echo "   11) 编辑配置"
        echo ""
        echo -e "  ${RED}其他:${NC}"
        echo "   12) 完全卸载"
        echo "    0) 退出"
        echo ""
        echo -n "请输入选项 [0-12]: "
        read -r choice
        
        echo ""
        
        case $choice in
            1)
                full_install
                ;;
            2)
                install_tdl
                ;;
            3)
                install_msgproce
                ;;
            4)
                start_console
                ;;
            5)
                install_service
                ;;
            6)
                stop_service
                ;;
            7)
                restart_service
                ;;
            8)
                check_environment
                ;;
            9)
                show_status
                ;;
            10)
                view_logs
                ;;
            11)
                edit_config
                ;;
            12)
                uninstall_all
                ;;
            0)
                echo -e "${GREEN}再见！${NC}"
                exit 0
                ;;
            *)
                echo -e "${RED}无效选项${NC}"
                ;;
        esac
        
        echo ""
        echo "按 Enter 键继续..."
        read -r
    done
}

# 启动主菜单
main_menu
