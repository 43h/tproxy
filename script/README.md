# systemd 系统服务

`tproxy.service` 是 Linux 客户端的服务模板，目录结构如下：

```text
tproxy/
├── bin/
│   ├── client
│   └── conf.yaml
└── script/
    ├── tproxy.service
    ├── install.sh
    └── uninstall.sh
```

先准备适合当前 Linux 系统的 `bin/client`（需有执行权限），并配置
`bin/conf.yaml`。在项目根目录执行：

```bash
sudo bash script/install.sh
```

安装脚本根据自身位置确定项目根目录，将模板中的 `@BIN_DIR@` 替换为
`bin` 的绝对路径，写入 `/etc/systemd/system/tproxy.service`，然后启动服务并
启用开机自启。从其他目录调用安装脚本也能正确定位 `bin`。
再次安装会更新服务文件并重启服务；移动项目目录后需重新安装。
项目路径支持空格，但不能包含控制字符、双引号、反斜杠或美元符号。

服务以 root 身份运行，以满足透明代理的权限要求，工作目录为 `bin`，
启动命令为 `client -c conf.yaml`，退出后自动重启。
程序日志按现有逻辑写入 `bin` 下的 `.log` 文件。
路由与 iptables 规则需按实际网络单独配置；安装脚本不会执行 `router.sh`。

常用管理命令：

```bash
sudo systemctl status tproxy.service
sudo systemctl restart tproxy.service
sudo systemctl stop tproxy.service
sudo journalctl -u tproxy.service -n 50 --no-pager
```

卸载服务：

```bash
sudo bash script/uninstall.sh
```

卸载会停止服务、取消开机自启并删除安装的服务文件，保留 `bin` 下的程序、
配置和日志。重复卸载会提示无需卸载。
