//go:build linux

package main

import (
	. "common"
	"flag"
	"fmt"
)

var logDebug = flag.Bool("d", false, "debug mode")
var version = flag.Bool("v", false, "print version and exit")
var help = flag.Bool("h", false, "print help and exit")

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

	if initServer() == false {
		return
	}

	go startClient()
	defer closeClient()

	startServer()
	defer closeServer()
}

func showHelp() {
	fmt.Println("Usage:")
	fmt.Println("  ./client -d")
	fmt.Println("  ./client -v")
	fmt.Println("  ./client -h")
	flag.PrintDefaults()
}
