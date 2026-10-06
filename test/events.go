package test

import (
	"fmt"

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
	Action    string
	Object    runtime.Object
	Related   runtime.Object
}

// Event records a new event in the slice of the fake event recorder used in a test
// The interface using Event is the EventRecorder interface
func (f *FakeEventRecorder) Eventf(object, related runtime.Object, eventtype, reason string, action string, messageFmt string, args ...any) {
	event := &EventData{
		Object:    object,
		Related:   related,
		EventType: eventtype,
		Reason:    reason,
		Action:    action,
		Message:   fmt.Sprintf(messageFmt, args...),
	}
	f.Events = append(f.Events, event)
}

// Reset resets the slice of the fake event recorder used in a test
func (f *FakeEventRecorder) Reset() {
	f.Events = []*EventData{}
}
