//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	. "tproxy/common"
)

var logDebug = flag.Bool("d", false, "debug mode")
var version = flag.Bool("v", false, "print version and exit")
var configFile = flag.String("c", "conf.yaml", "configuration file")

func main() {
	flag.Parse()

	if *version {
		fmt.Println(ShowVersion())
		return
	}

	if !InitLog(*logDebug) {
		return
	}
	defer CloseLog()

	if !initConf(*configFile) {
		return
	}

	app := NewRelayApp(&ConfigParam)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Run()
	}()

	select {
	case err := <-errChan:
		if err != nil {
			LOGE("Application error: ", err)
		}
	case sig := <-sigChan:
		LOGI("Received signal: ", sig)
	}

	app.Shutdown()
}