//go:build linux

package main

import (
	. "tproxy/common"
	"flag"
	"fmt"
)

var logDebug = flag.Bool("d", false, "debug mode")
var version = flag.Bool("v", false, "print version and exit")
var help = flag.Bool("h", false, "print help and exit")

var serverInfo ServerInfo

func main() {
	flag.Parse()
	if *version {
		fmt.Println(ShowVersion())
		return
	}
	if *help {
		showHelp()
		return
	}
	if InitLog(*logDebug) == false {
		return
	}
	defer CloseLog()

	if initConf() == false {
		return
	}

	initProxy()
	
	serverInfo.ServerAddr = ConfigParam.Listen
	if serverInfo.initServer() == false {
		return
	}

	go startClient()
	defer closeProxy()

	serverInfo.startServer()
	defer serverInfo.closeServer()
}

func showHelp() {
	fmt.Println("Usage:")
	fmt.Println("  ./client -d")
	fmt.Println("  ./client -v")
	fmt.Println("  ./client -h")
	flag.PrintDefaults()
}
