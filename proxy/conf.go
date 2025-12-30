//go:build linux

package main

import (
	. "tproxy/common"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Listen string `yaml:"listen"` //local listen
	Server string `yaml:"server"` //remote server
}

var ConfigParam = Config{"", ""}

func initConf(configFile string) bool {
	data := LoadConf(configFile)
	if data == nil {
		return false
	}

	err := yaml.Unmarshal(data, &ConfigParam)
	if err != nil {
		LOGE(err)
		return false
	}

	LOGI("config: ", ConfigParam)
	return true
}