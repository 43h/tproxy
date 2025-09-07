//go:build windows

package main

import (
	. "common"
	"flag"
	"fmt"
)

var logDebug = flag.Bool("d", false, "debug mode")
var version = flag.Bool("v", false, "print version and exit")

func main() {
	flag.Parse()
	if *version {
		fmt.Println(ShowVersion())
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
	startServer()
	defer closeServer()
}
