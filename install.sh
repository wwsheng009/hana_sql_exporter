#!/bin/bash

# 全局变量
INSTALL_DIR="/opt/exporter/hana_sql_exporter"
SERVICE_FILE="/etc/systemd/system/hana_sql_exporter@.service"

# 显示帮助信息
show_help() {
    echo "用法: $0 [选项] [本地安装包路径]"
    echo "选项:"
    echo "  -h, --help     显示此帮助信息"
    echo "  -l, --local    使用本地安装包安装"
    echo "示例:"
    echo "  $0             在线下载并安装最新版本"
    echo "  $0 -l /path/to/hana_sql_exporter.tar.gz  使用本地安装包安装"
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

# 解析命令行参数
parse_arguments() {
    local -n local_install=$1
    local -n local_package=$2

    while [[ $# -gt 2 ]]; do
        case ${3} in
            -h|--help)
                show_help
                exit 0
                ;;
            -l|--local)
                local_install=true
                if [[ -n "${4}" && ! ${4} =~ ^- ]]; then
                    local_package="${4}"
                    shift
                fi
                ;;
            *)
                if [[ ${local_install} = true && -z "${local_package}" ]]; then
                    local_package="${3}"
                else
                    echo "错误：未知参数 ${3}"
                    show_help
                    exit 1
                fi
                ;;
        esac
        shift
    done
}

# 本地安装处理
handle_local_install() {
    local package=$1
    
    if [ -z "${package}" ]; then
        echo "错误：本地安装模式需要指定安装包路径"
        show_help
        exit 1
    fi

    if [ ! -f "${package}" ]; then
        echo "错误：找不到安装包文件：${package}"
        exit 1
    fi

    echo "正在使用本地安装包安装..."
    cp "${package}" "${INSTALL_DIR}/hana_sql_exporter.tar.gz"
}

# 在线安装处理
handle_online_install() {
    local arch=$1
    
    echo "正在获取最新版本信息..."
    local latest_version=$(curl -s https://api.github.com/repos/wwsheng009/hana_sql_exporter/releases/latest | grep '"tag_name":' | cut -d'"' -f4)
    if [ -z "${latest_version}" ]; then
        echo "无法获取最新版本信息"
        exit 1
    fi

    local download_url="https://github.com/wwsheng009/hana_sql_exporter/releases/download/${latest_version}/hana_sql_exporter_linux_${arch}.tar.gz"
    echo "正在下载: ${download_url}"
    if ! curl -L -o "${INSTALL_DIR}/hana_sql_exporter.tar.gz" "${download_url}"; then
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
    if ! tar xzf hana_sql_exporter.tar.gz; then
        echo "解压失败"
        rm -f hana_sql_exporter.tar.gz
        exit 1
    fi

    chmod +x hana_sql_exporter

    # 检查配置文件
    if [ ! -f "hana_sql_exporter.toml" ]; then
        echo "警告：未找到配置文件，请确保在启动服务前创建并配置 hana_sql_exporter.toml"
    fi

    # 安装服务文件
    if [ -f "hana_sql_exporter@.service" ]; then
        cp hana_sql_exporter@.service "${SERVICE_FILE}"
        echo "已安装服务文件"
    else
        echo "警告：未找到服务文件 hana_sql_exporter@.service"
    fi

    # 清理临时文件
    rm -f hana_sql_exporter.tar.gz
}

# 配置系统服务
configure_service() {
    systemctl daemon-reload

    echo "\n安装完成！"
    echo "请确保配置文件 ${INSTALL_DIR}/hana_sql_exporter.toml 已正确设置"
    echo "\n使用以下命令启动服务："
    echo "systemctl start hana_sql_exporter@<instance>"
    echo "\n使用以下命令设置开机自启："
    echo "systemctl enable hana_sql_exporter@<instance>"
}

# 主函数
main() {
    local LOCAL_INSTALL=false
    local LOCAL_PACKAGE=""

    # 解析参数
    parse_arguments LOCAL_INSTALL LOCAL_PACKAGE "$@"

    # 检查系统要求
    ARCH=$(check_system_requirements)

    # 根据安装模式处理
    if [ "${LOCAL_INSTALL}" = true ]; then
        handle_local_install "${LOCAL_PACKAGE}"
    else
        handle_online_install "${ARCH}"
    fi

    # 安装文件
    install_files

    # 配置服务
    configure_service
}

# 执行主函数
main "$@"