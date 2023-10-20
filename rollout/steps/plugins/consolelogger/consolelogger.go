package consolelogger

import (
	"encoding/json"
	"fmt"
	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"log"
	"time"
)

type ConsoleLogger struct {
}

type RunStatus struct {
	IsRunning          bool
	IsCompleted        bool
	TimeRunningStarted time.Time
	TimeRunning        time.Duration
	DummyStruct        DummyStruct
	Count              int
}

type DummyStruct struct {
	Value1 string
	Value2 string
}

func NewConsoleLoggerStep() *ConsoleLogger {
	return &ConsoleLogger{}
}

func (c *ConsoleLogger) RunStep(rollout rolloutsv1alpha1.Rollout) (json.RawMessage, error) {
	log.Printf("Running ConsoleLogger on Rollout %s", rollout.Name)

	for _, status := range rollout.Status.StepPluginStatuses {
		if status.Name == fmt.Sprintf("%s.%d", c.Type(), *rollout.Status.CurrentStepIndex) {
			runS := RunStatus{}
			if err := json.Unmarshal(status.Status, &runS); err != nil {
				return nil, fmt.Errorf("failed to unmarshal plugin IsRunning response: %w", err)
			}

			//var byteStatus []byte
			if runS.IsRunning == true && runS.IsCompleted == false {
				//byteStatus, _ = json.Marshal(status)
				return nil, nil
			}
		}
	}

	byteStatus, _ := json.Marshal(RunStatus{
		IsRunning:          true,
		IsCompleted:        false,
		TimeRunningStarted: time.Now(),
		DummyStruct: DummyStruct{
			Value1: "Value1",
			Value2: "Value2",
		},
		Count: 0,
	})
	return byteStatus, nil
}

func (c *ConsoleLogger) IsStepCompleted(rollout rolloutsv1alpha1.Rollout) (bool, json.RawMessage, error) {

	rs := RunStatus{}
	for _, status := range rollout.Status.StepPluginStatuses {
		var stepIndex int32 = 0
		if rollout.Status.CurrentStepIndex != nil {
			stepIndex = *rollout.Status.CurrentStepIndex
		}
		if status.Name == fmt.Sprintf("%s.%d", c.Type(), stepIndex) {
			err := json.Unmarshal(status.Status, &rs)
			if err != nil {
				return false, nil, fmt.Errorf("failed to unmarshal plugin IsRunning response: %w", err)
			}
			if rs.IsRunning == true {
				rs.Count++
			} else {
				rs.IsRunning = false
			}

			byteStatus, _ := json.Marshal(rs)

			if rs.Count >= 10 {
				log.Printf("ConsoleLogger Step Completed")
				return true, byteStatus, nil
			}

			return rs.Count >= 10, byteStatus, nil
		}
	}

	return false, nil, nil
}

func (c *ConsoleLogger) Type() string {
	return "consolelogger"
}
