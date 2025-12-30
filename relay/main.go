//go:build windows

package main

import (
	. "tproxy/common"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var logDebug = flag.Bool("d", false, "debug mode")
var version = flag.Bool("v", false, "print version and exit")

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

	if !initConf() {
		return
	}

	// 创建应用
	app := NewRelayApp(&ConfigParam)

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 在goroutine中运行应用
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Run()
	}()

	// 等待信号或错误
	select {
	case err := <-errChan:
		if err != nil {
			LOGE("Application error: ", err)
		}
	case sig := <-sigChan:
		LOGI("Received signal: ", sig)
	}

	// 优雅关闭
	app.Shutdown()
}
