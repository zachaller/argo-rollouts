package sync

import "sync"

var ranOnceCheck map[string]state = make(map[string]state)
var mutex sync.RWMutex

type state struct {
	running bool
	ran     bool
}

func IsRunning(key string) bool {
	mutex.RLock()
	defer mutex.RUnlock()
	return ranOnceCheck[key].running && !ranOnceCheck[key].ran
}

func HasRun(key string) bool {
	mutex.RLock()
	defer mutex.RUnlock()
	return !ranOnceCheck[key].running && ranOnceCheck[key].ran
}

func StartRunning(key string) {
	mutex.Lock()
	defer mutex.Unlock()
	cstate := ranOnceCheck[key]
	cstate.running = true
	ranOnceCheck[key] = cstate

}

func StoppedRunning(key string) {
	mutex.Lock()
	defer mutex.Unlock()
	cstate := ranOnceCheck[key]
	cstate.running = false
	cstate.ran = true
	ranOnceCheck[key] = cstate
}
