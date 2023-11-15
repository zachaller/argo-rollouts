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
	//DummyStruct        DummyStruct
	Count int
}

type DummyStruct struct {
	Value1 string
	Value2 string
}

func NewConsoleLoggerStep() *ConsoleLogger {
	return &ConsoleLogger{}
}

func (c *ConsoleLogger) RunStep(rollout rolloutsv1alpha1.Rollout, currentStatus rolloutsv1alpha1.StepPluginStatuses) (json.RawMessage, error) {
	if currentStatus.IsEmpty() {
		byteStatus, _ := json.Marshal(RunStatus{
			IsRunning:          true,
			IsCompleted:        false,
			TimeRunningStarted: time.Now(),
			Count:              0,
			//DummyStruct: DummyStruct{
			//	Value1: "Value1",
			//	Value2: "Value2",
			//},
		})

		go func() {
			for {
				log.Printf("Running ConsoleLogger on Rollout %s", rollout.Name)
				time.Sleep(5 * time.Second)
			}
		}()

		return byteStatus, nil
	} else {
		runStatus := RunStatus{}
		err := json.Unmarshal(currentStatus.Status.Value, &runStatus)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal plugin status %s: %w", currentStatus.Status, err)
		}
		if runStatus.IsCompleted == false {
			go func() {
				for {
					log.Printf("Running ConsoleLogger on Rollout %s", rollout.Name)
					time.Sleep(5 * time.Second)
				}
			}()
		}

		byteStatus, _ := json.Marshal(runStatus)
		return byteStatus, nil
	}
}

func (c *ConsoleLogger) IsStepCompleted(rollout rolloutsv1alpha1.Rollout, currentStatus rolloutsv1alpha1.StepPluginStatuses) (bool, json.RawMessage, error) {

	rs := RunStatus{}
	json.Unmarshal(currentStatus.Status.Value, &rs)

	rs.Count++

	byteStatus, _ := json.Marshal(rs)

	if rs.Count >= 10 {
		log.Printf("ConsoleLogger Step Completed")
		return true, byteStatus, nil
	}

	return rs.Count >= 10, byteStatus, nil
}

func (c *ConsoleLogger) Type() string {
	return "consolelogger"
}
