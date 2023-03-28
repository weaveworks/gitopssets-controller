package test

import (
	"k8s.io/apimachinery/pkg/runtime"
)

type FakeEventRecorder struct {
	Events []*EventData
}

type EventData struct {
	EventType string
	Reason    string
	Message   string
}

func (f *FakeEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	event := &EventData{
		EventType: eventtype,
		Reason:    reason,
		Message:   message,
	}
	f.Events = append(f.Events, event)
}
func (f *FakeEventRecorder) Reset() {
	f.Events = []*EventData{}
}
