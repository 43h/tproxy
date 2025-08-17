#!/bin/bash

ip rule del fwmark 1 lookup 100
ip route del local 0.0.0.0/0 dev lo table 100
iptables -t mangle -F

iptables -t mangle -A PREROUTING -p tcp -d 10.0.0.0/8 --dport 22 -j TPROXY --tproxy-mark 0x1/0x1 --on-port 8080 --on-ip 192.168.3.50

ip rule add fwmark 1 lookup 100
ip route add local 0.0.0.0/0 dev lo table 100
