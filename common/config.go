package common

import (
	"os"
)

func LoadConf(configFile string) []byte {
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		LOGE(configFile, " does not exist")
		return nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		LOGE("fail to load ", configFile, err)
		return nil
	}
	return data
}