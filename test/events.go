package test

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// FakeEventRecorder is a fake event recorder used in tests to simulate recording the events in a slice of EventData
type FakeEventRecorder struct {
	Events []*EventData
}

// EventData represents the data of a fake event used in tests
type EventData struct {
	EventType string
	Reason    string
	Message   string
}

// Event records a new event in the slice of the fake event recorder used in a test
// The interface using Event is the EventRecorder interface
func (f *FakeEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	event := &EventData{
		EventType: eventtype,
		Reason:    reason,
		Message:   message,
	}
	f.Events = append(f.Events, event)
}

// Reset resets the slice of the fake event recorder used in a test
func (f *FakeEventRecorder) Reset() {
	f.Events = []*EventData{}
}
