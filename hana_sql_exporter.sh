#!/bin/bash

# 全局变量
INSTALL_DIR="/opt/exporter/hana_sql_exporter"
# 定义版本
VERSION=""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# 显示帮助信息
show_help() {
    echo "用法: $0"
    echo "此脚本将引导您完成 HANA SQL Exporter 的安装过程"
    echo "密码配置命令:"
    echo "  $ ./hana_sql_exporter pw --tenant q01 --config ./<instance>.toml"
    echo "  $ ./hana_sql_exporter pw -t qj1 -c ./<instance>.toml"
    echo "  $ ./hana_sql_exporter pw --tenant q01,qj1 --config ./<instance>.toml"
}

# 交互式菜单
interactive_menu() {
    clear
    echo -e "${GREEN}=== HANA SQL Exporter 安装程序 ===${NC}"
    echo "1. 使用局域网安装包安装"
    echo "2. 使用最新安装包安装"
    echo "3. 使用本地安装包安装"
    echo "4. 退出"
    echo -n "请选择安装方式 [1-4]: "
    
    read -r choice
    case $choice in
        1)
            handle_fetch_local
            install_files
            configure_service
            ;;
        2)
            handle_fetch_latest $(check_system_requirements)
            install_files
            configure_service
            ;;
        3)
            echo -n "请输入本地安装包路径: "
            read -r package_path
            if [ -z "$package_path" ]; then
                echo -e "${RED}错误: 必须提供本地安装包路径${NC}"
                exit 1
            fi
            handle_local_install "$package_path"
            install_files
            configure_service
            ;;
        4)
            echo "退出安装程序"
            exit 0
            ;;
        *)
            echo -e "${RED}错误: 无效的选择${NC}"
            sleep 1
            interactive_menu
            ;;
    esac
}

# 检查系统要求
check_system_requirements() {
    # 检查root权限
    if [ "$(id -u)" != "0" ]; then
        echo "此脚本需要root权限运行"
        exit 1
    fi

    # 检测系统架构
    local arch=$(uname -m)
    case ${arch} in
        x86_64)
            echo "amd64"
            ;;
        aarch64)
            echo "arm64"
            ;;
        *)
            echo "不支持的系统架构: ${arch}"
            exit 1
            ;;
    esac
}

handle_fetch_local() {
    # 检查下载工具是否存在
    if command -v wget &> /dev/null; then
        DOWNLOADER="wget"
    elif command -v curl &> /dev/null; then
        DOWNLOADER="curl"
    else
        echo -e "${RED}错误：系统中既没有安装 wget 也没有安装 curl，请先安装其中一个工具${NC}"
        echo "Ubuntu/Debian: sudo apt-get install wget 或 sudo apt-get install curl"
        echo "CentOS/RHEL: sudo yum install wget 或 sudo yum install curl"
        return 1
    fi

    # 提示用户输入监控主机地址
    echo "请输入监控主机的IP地址或主机名:"
    read -r HOST
    if [ -z "$HOST" ]; then
        echo "错误：监控主机地址不能为空"
        return 1
    fi

    # 创建目录
    echo -e "${YELLOW}正在创建安装目录...${NC}"
    mkdir -p "${INSTALL_DIR}"
    
    # 检查目录是否创建成功
    if [ ! -d "${INSTALL_DIR}" ]; then
        echo -e "${RED}错误：无法创建安装目录 ${INSTALL_DIR}${NC}"
        return 1
    fi
    
    echo -e "${GREEN}安装目录创建成功${NC}"

    # 使用可用的下载工具下载文件
    if [ "$DOWNLOADER" = "wget" ]; then
        wget http://${HOST}/n9e_install_files/${VERSION} -P ${INSTALL_DIR}/
    elif [ "$DOWNLOADER" = "curl" ]; then
        curl -L http://${HOST}/n9e_install_files/${VERSION} -o ${INSTALL_DIR}/${VERSION}
    fi
}

# 本地安装处理
handle_local_install() {
    local package=$1
    
    if [ ! -f "${package}" ]; then
        echo "错误：找不到安装包文件：${package}"
        exit 1
    fi

    echo "正在使用本地安装包安装..."
    cp "${package}" "${INSTALL_DIR}/${VERSION}"
}

# 在线安装处理
handle_fetch_latest() {
    local arch=$1
    
    echo "正在获取最新版本信息..."
    local latest_version=$(curl -s https://api.github.com/repos/wwsheng009/hana_sql_exporter/releases/latest | grep '"tag_name":' | cut -d'"' -f4)
    if [ -z "${latest_version}" ]; then
        echo "无法获取最新版本信息"
        exit 1
    fi

    local download_url="https://github.com/wwsheng009/hana_sql_exporter/releases/download/${latest_version}/hana_sql_exporter_linux_${arch}.tar.gz"
    echo "正在下载: ${download_url}"
    if ! curl -L -o "${INSTALL_DIR}/${VERSION}" "${download_url}"; then
        echo "下载失败"
        exit 1
    fi
}

# 安装文件处理
install_files() {
    # 创建安装目录
    mkdir -p "${INSTALL_DIR}"
    cd "${INSTALL_DIR}"

    # 解压文件
    if ! tar xzf ${VERSION}; then
        echo "解压失败"
        exit 1
    fi

    chmod +x hana_sql_exporter

    # 检查配置文件
    if [ ! -f "hana_sql_exporter.toml" ]; then
        echo "警告：未找到配置文件，请确保在启动服务前创建并配置 hana_sql_exporter.toml"
    fi

    # 安装服务文件
    if [ -f "hana_sql_exporter@.service" ]; then
        cp hana_sql_exporter@.service /etc/systemd/system/hana_sql_exporter@.service
        echo "已安装服务文件"
    else
        echo "警告：未找到服务文件 hana_sql_exporter@.service"
    fi
}

# 配置系统服务
configure_service() {
    systemctl daemon-reload

    echo "安装完成！"
    echo "请确保配置文件 ${INSTALL_DIR}/<instance>.toml 已正确设置"
    echo "使用以下命令启动服务："
    echo "systemctl start hana_sql_exporter@<instance>"
    echo "使用以下命令设置开机自启："
    echo "systemctl enable hana_sql_exporter@<instance>"
}

# 主函数
main() {
    # 检查系统要求
    ARCH=$(check_system_requirements)
    VERSION="hana_sql_exporter_linux_${ARCH}.tar.gz"
    
    # 显示帮助信息
    show_help
    
    # 直接进入交互式菜单
    interactive_menu
}

# 执行主函数
main