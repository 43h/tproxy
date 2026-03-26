package main

import (
	. "tproxy/common"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Listen           string   `yaml:"listen"`
	Webhook          string   `yaml:"webhook"`
	WebhookMention   []string `yaml:"webhook_mention"`
	VpnCheckInterval int      `yaml:"check_interval"`
}

var ConfigParam = Config{}

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
