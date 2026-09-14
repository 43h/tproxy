#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME=tproxy.service
UNIT_FILE=/etc/systemd/system/$SERVICE_NAME

fail() {
    printf '错误：%s\n' "$*" >&2
    exit 1
}

[[ $(id -u) -eq 0 ]] || fail "请使用 sudo bash script/uninstall.sh 卸载服务。"
command -v systemctl >/dev/null || fail "未找到 systemctl，请在 systemd 系统上运行。"
[[ -d /run/systemd/system ]] || fail "当前系统未运行 systemd。"

if [[ ! -e "$UNIT_FILE" && ! -L "$UNIT_FILE" ]]; then
    printf '未找到 %s，无需卸载。\n' "$UNIT_FILE"
    exit 0
fi

systemctl disable --now "$SERVICE_NAME"
rm -f -- "$UNIT_FILE"
systemctl daemon-reload
printf '已卸载 %s，bin 下的程序、配置和日志已保留。\n' "$SERVICE_NAME"
