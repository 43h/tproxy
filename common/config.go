package common

import (
	"io/ioutil"
	"os"
)

const confFile = "conf.yaml"

func LoadConf() []byte {
	if _, err := os.Stat(confFile); os.IsNotExist(err) {
		LOGE("conf.yaml does not exist")
		return nil
	}

	data, err := ioutil.ReadFile(confFile)
	if err != nil {
		LOGE("fail to load ", confFile, err)
		return nil
	}
	return data
}
