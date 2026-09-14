#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
BIN_DIR=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/bin
SERVICE_NAME=tproxy.service
UNIT_FILE=/etc/systemd/system/$SERVICE_NAME

fail() {
    printf '错误：%s\n' "$*" >&2
    exit 1
}

[[ $(id -u) -eq 0 ]] || fail "请使用 sudo bash $SCRIPT_DIR/install.sh 安装服务。"
command -v systemctl >/dev/null || fail "未找到 systemctl，请在 systemd 系统上运行。"
[[ -d /run/systemd/system ]] || fail "当前系统未运行 systemd。"
[[ -f "$BIN_DIR/client" && -x "$BIN_DIR/client" ]] || fail "缺少可执行文件：$BIN_DIR/client"
[[ -f "$BIN_DIR/conf.yaml" && -r "$BIN_DIR/conf.yaml" ]] || fail "缺少可读配置：$BIN_DIR/conf.yaml"
[[ -f "$SCRIPT_DIR/$SERVICE_NAME" ]] || fail "缺少服务模板：$SCRIPT_DIR/$SERVICE_NAME"
# systemd restricts special characters in executable paths.
[[ "$BIN_DIR" != *[[:cntrl:]]* && "$BIN_DIR" != *\"* && "$BIN_DIR" != *\\* && "$BIN_DIR" != *\$* ]] || fail '项目路径不能包含控制字符、双引号、反斜杠或美元符号。'

# WorkingDirectory is a literal path with systemd specifier expansion.
unit_bin_dir=${BIN_DIR//%/%%}
temp_unit=$(mktemp)
trap 'rm -f -- "$temp_unit"' EXIT
while IFS= read -r line || [[ -n "$line" ]]; do
    line=${line//@BIN_DIR@/"$unit_bin_dir"}
    printf '%s\n' "$line"
done < "$SCRIPT_DIR/$SERVICE_NAME" > "$temp_unit"

install -m 0644 -- "$temp_unit" "$UNIT_FILE"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"
systemctl --no-pager --full status "$SERVICE_NAME"
printf '已安装并启动 %s，已启用开机自启。工作目录：%s\n' "$SERVICE_NAME" "$BIN_DIR"
