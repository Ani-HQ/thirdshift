package state

import "fmt"

type State string

const (
	Unregistered State = "UNREGISTERED"
	Registering  State = "REGISTERING"
	Offline      State = "OFFLINE"
	Starting     State = "STARTING"
	Benchmarking State = "BENCHMARKING"
	Idle         State = "IDLE"
	Preparing    State = "PREPARING_MODEL"
	Available    State = "AVAILABLE"
	Busy         State = "BUSY"
	Draining     State = "DRAINING"
	Paused       State = "PAUSED"
	Error        State = "ERROR"
	Updating     State = "UPDATING"
)

var legalTransitions = map[State]map[State]bool{
	Unregistered: {Registering: true},
	Registering:  {Offline: true, Error: true},
	Offline:      {Starting: true, Updating: true, Error: true},
	Starting:     {Benchmarking: true, Idle: true, Preparing: true, Paused: true, Draining: true, Error: true, Updating: true, Offline: true},
	Benchmarking: {Idle: true, Preparing: true, Paused: true, Draining: true, Error: true, Updating: true, Offline: true},
	Idle:         {Preparing: true, Paused: true, Draining: true, Error: true, Updating: true, Offline: true},
	Preparing:    {Available: true, Paused: true, Draining: true, Error: true, Updating: true, Offline: true},
	Available:    {Busy: true, Draining: true, Paused: true, Error: true, Updating: true, Offline: true},
	Busy:         {Available: true, Draining: true, Error: true, Updating: true, Offline: true},
	Draining:     {Available: true, Paused: true, Error: true, Updating: true, Offline: true},
	Paused:       {Idle: true, Preparing: true, Draining: true, Error: true, Updating: true, Offline: true},
	Error:        {Offline: true, Starting: true, Updating: true},
	Updating:     {Starting: true, Offline: true, Error: true},
}

func Parse(value string) (State, error) {
	state := State(value)
	if !IsKnown(state) {
		return "", fmt.Errorf("unknown node state %q", value)
	}
	return state, nil
}

func IsKnown(state State) bool {
	_, ok := legalTransitions[state]
	return ok
}

func CanTransition(from, to State) bool {
	if from == to {
		return true
	}
	return legalTransitions[from][to]
}

func Transition(from, to State) error {
	if !IsKnown(from) {
		return fmt.Errorf("unknown source state %q", from)
	}
	if !IsKnown(to) {
		return fmt.Errorf("unknown destination state %q", to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("illegal node state transition %s -> %s", from, to)
	}
	return nil
}

func Active(state State) bool {
	switch state {
	case Starting, Benchmarking, Idle, Preparing, Available, Busy, Draining, Paused, Error, Updating:
		return true
	default:
		return false
	}
}
