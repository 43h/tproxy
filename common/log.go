package common

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DEBUG = iota
	INFO
	ERROR
)

var logLevel = INFO
var logHandle *os.File

func LOGD(v ...interface{}) {
	if logLevel <= DEBUG {
		log.Println("[ DEBUG] ", v)
	}
}

func LOGI(v ...interface{}) {
	if logLevel <= INFO {
		log.Println("[  INFO] ", v)
	}
}

func LOGE(v ...interface{}) {
	if logLevel <= ERROR {
		log.Println("[ERROR] ", v)
	}
}

func InitLog(debug bool) bool {
	DelLog()

	if debug == true { //debug mode
		logLevel = DEBUG
		return true
	}

	fileName := time.Now().Format("2006-01-02-15-04-05") + ".log"
	logHandle, err := os.OpenFile(fileName, os.O_APPEND|os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		fmt.Println("fail to open file, ", err)
		return false
	}

	log.SetOutput(logHandle)
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	return true
}

func CloseLog() {
	if logHandle != nil {
		err := logHandle.Close()
		if err != nil {
			LOGE("fail to close log file, ", err)
		}
	}
}

func DelLog() {
	dir := "./"
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".log") {
			err := os.Remove(path)
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		LOGE("fail to del log, ", err)
	}
}