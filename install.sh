#!/bin/bash

# 检查是否以root权限运行
if [ "$(id -u)" != "0" ]; then
    echo "此脚本需要root权限运行"
    exit 1
fi

# 检测系统架构
ARCH=$(uname -m)
case ${ARCH} in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
    *)
        echo "不支持的系统架构: ${ARCH}"
        exit 1
        ;;
esac

# 设置安装目录
INSTALL_DIR="/opt/exporter/hana_sql_exporter"
SERVICE_FILE="/etc/systemd/system/hana_sql_exporter@.service"

# 创建安装目录
mkdir -p "${INSTALL_DIR}"

# 获取最新版本
LATEST_VERSION=$(curl -s https://api.github.com/repos/wwsheng009/hana_sql_exporter/releases/latest | grep '"tag_name":' | cut -d'"' -f4)
if [ -z "${LATEST_VERSION}" ]; then
    echo "无法获取最新版本信息"
    exit 1
fi

# 下载并解压文件
DOWNLOAD_URL="https://github.com/wwsheng009/hana_sql_exporter/releases/download/${LATEST_VERSION}/hana_sql_exporter_linux_${ARCH}.tar.gz"
echo "正在下载: ${DOWNLOAD_URL}"
if ! curl -L -o "${INSTALL_DIR}/hana_sql_exporter.tar.gz" "${DOWNLOAD_URL}"; then
    echo "下载失败"
    exit 1
fi

# 解压文件
cd "${INSTALL_DIR}"
tar xzf hana_sql_exporter.tar.gz
chmod +x hana_sql_exporter

# 检查配置文件是否存在
if [ ! -f "hana_sql_exporter.toml" ]; then
    echo "警告：未找到配置文件"
fi

# 复制服务文件到系统目录
if [ -f "hana_sql_exporter@.service" ]; then
    cp hana_sql_exporter@.service "${SERVICE_FILE}"
else
    echo "警告：未找到服务文件"
fi

# 清理临时文件
rm -f hana_sql_exporter.tar.gz

# 重新加载systemd配置
systemctl daemon-reload

echo "安装完成！"
echo "请确保配置文件 ${INSTALL_DIR}/hana_sql_exporter.toml 已正确设置"
echo "使用以下命令启动服务："
echo "systemctl start hana_sql_exporter@<instance>"
echo "使用以下命令设置开机自启："
echo "systemctl enable hana_sql_exporter@<instance>"