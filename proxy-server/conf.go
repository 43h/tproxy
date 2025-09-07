//go:build windows

package main

import (
	. "common"

	"gopkg.in/yaml.v2"
)

const confFile = "conf.yaml"

type Config struct {
	Listen string `yaml:"listen"`
}

var ConfigParam = Config{""}

func initConf() bool {
	data := LoadConf()
	if data == nil {
		return false
	}

	err := yaml.Unmarshal(data, &ConfigParam)
	if err != nil {
		LOGE(err)
		return false
	} else {
		LOGI("config: ", ConfigParam)
		return true
	}
}
