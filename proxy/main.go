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
var help = flag.Bool("h", false, "print help and exit")
var configFile = flag.String("c", "conf.yaml", "configuration file")

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
	if !InitLog(*logDebug) {
		return
	}
	defer CloseLog()

	if !initConf(*configFile) {
		return
	}

	app := NewProxyApp(&ConfigParam)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)

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

func showHelp() {
	fmt.Println("Usage:")
	fmt.Println("  ./client -d          # Run in debug mode")
	fmt.Println("  ./client -v          # Show version")
	fmt.Println("  ./client -h          # Show this help")
	fmt.Println("  ./client -c [config_file]  # Run in config file")
	fmt.Println()
	flag.PrintDefaults()
}
