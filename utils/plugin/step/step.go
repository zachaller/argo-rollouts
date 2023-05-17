package step

import (
	"fmt"
	"strconv"
	"strings"
)

type PluginStatusKey struct {
	PluginName string
	StepIndex  int32
}

func (psk *PluginStatusKey) String() string {
	return fmt.Sprintf("%s:%d", psk.PluginName, psk.StepIndex)
}

func (psk *PluginStatusKey) Parse(s string) error {
	psk.PluginName = strings.Split(s, ":")[0]
	stepIdx, err := strconv.Atoi(strings.Split(s, ":")[1])
	if err != nil {
		return err
	}
	psk.StepIndex = int32(stepIdx)
	return nil
}
