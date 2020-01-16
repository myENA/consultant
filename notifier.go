package consultant

import (
	"fmt"
	"sync"
	"time"
)

// NotificationSource identifies the source of the notification push
type NotificationSource uint8

const (
	NotificationSourceManagedSession NotificationSource = iota
	NotificationSourceCandidate
	NotificationSourceManagedService

	NotificationSourceTest NotificationSource = 0xf
)

func (ns NotificationSource) String() string {
	switch ns {
	case NotificationSourceManagedSession:
		return "ManagedSession"
	case NotificationSourceCandidate:
		return "Candidate"
	case NotificationSourceManagedService:
		return "ManagedService"

	case NotificationSourceTest:
		return "Test"

	default:
		return "UNKNOWN"
	}
}

// NotificationEvent describes the reason behind a given notification push
type NotificationEvent uint64

const (
	NotificationEventManualPush NotificationEvent = 0x0 // sent whenever a manual notification is requested
	NotificationEventTestPush   NotificationEvent = 0xf // used for testing purposes

	NotificationEventManagedSessionRunning NotificationEvent = 0x80 // sent when session is running
	NotificationEventManagedSessionStopped NotificationEvent = 0x81 // sent when session is no longer running
	NotificationEventManagedSessionCreate  NotificationEvent = 0x82 // sent after an attempt to create an upstream consul session
	NotificationEventManagedSessionRenew   NotificationEvent = 0x83 // sent after a renew attempt on a previously created upstream consul session
	NotificationEventManagedSessionDestroy NotificationEvent = 0x84 // sent after a destroy attempt on a previously created upstream consul session

	NotificationEventCandidateRunning      NotificationEvent = 0x100 // sent when candidate enters running
	NotificationEventCandidateStopped      NotificationEvent = 0x101 // sent when candidate leaves running
	NotificationEventCandidateElected      NotificationEvent = 0x102 // sent when candidate has been "elected"
	NotificationEventCandidateLostElection NotificationEvent = 0x103 // sent when candidate lost election
	NotificationEventCandidateResigned     NotificationEvent = 0x104 // sent when candidate explicitly "resigns"
	NotificationEventCandidateRenew        NotificationEvent = 0x105 // sent when candidate was previously elected and attempts to stay elected

	NotificationEventManagedServiceRunning          NotificationEvent = 0x180 // sent when the service has reached its "running" state
	NotificationEventManagedServiceStopped          NotificationEvent = 0x181 // sent when the service is considered defunct
	NotificationEventManagedServiceWatchPlanStarted NotificationEvent = 0x182 // sent when the internal watch plan for the service is running
	NotificationEventManagedServiceWatchPlanStopped NotificationEvent = 0x182 // sent when the internal watch plan for the service has stopped
	NotificationEventManagedServiceRefreshed        NotificationEvent = 0x183 // sent when the internal watch plan receives an update
)

func (ev NotificationEvent) String() string {
	switch ev {
	case NotificationEventManualPush:
		return "ManualPush"
	case NotificationEventTestPush:
		return "TestPush"

	case NotificationEventManagedSessionStopped:
		return "ManagedSessionStopped"
	case NotificationEventManagedSessionRunning:
		return "ManagedSessionRunning"
	case NotificationEventManagedSessionCreate:
		return "ManagedSessionCreate"
	case NotificationEventManagedSessionRenew:
		return "ManagedSessionRenew"
	case NotificationEventManagedSessionDestroy:
		return "ManagedSessionDestroy"

	case NotificationEventCandidateStopped:
		return "CandidateStopped"
	case NotificationEventCandidateRunning:
		return "CandidateRunning"
	case NotificationEventCandidateElected:
		return "CandidateElected"
	case NotificationEventCandidateResigned:
		return "CandidateResigned"
	case NotificationEventCandidateRenew:
		return "CandidateRenew"

	default:
		return "UNKNOWN_EVENT"
	}
}

// Notification describes a specific event with associated data that gets pushed to any registered recipients at the
// time of push
type Notification struct {
	ID         string // random
	Originated int64  // unixnano timestamp of when this notification was created
	Source     NotificationSource
	Event      NotificationEvent
	Data       interface{} // no attempt is made to prevent memory sharing
}

// NotificationHandler can be provided to a Notifier to be called per Notification
type NotificationHandler func(Notification)

// NotificationChannel can be provided to a Notifier to have new Notifications pushed to it
type NotificationChannel chan<- Notification

// Notifier represents a type within Consultant that can push in-process notifications to things.
type Notifier interface {
	// AttachNotificationHandler must immediately add the provided fn to the list of recipients for new notifications.
	//
	// It must:
	// - panic if fn is nil
	// - generate random ID if provided ID is empty
	// - return "true" if there was an existing recipient with the same identifier
	AttachNotificationHandler(id string, fn NotificationHandler) (actualID string, replaced bool)

	// AttachNotificationChannel must immediately add the provided channel to the list of recipients for new
	// notifications.
	//
	// It must:
	// - panic if ch is nil
	// - generate random ID if provided ID is empty
	// - return "true" if there was an existing recipient with the same identifier
	AttachNotificationChannel(id string, ch NotificationChannel) (actualID string, replaced bool)

	// DetachNotificationRecipient must immediately remove the provided ID from the list of recipients for new
	// notifications, if exists.  It will return true if a recipient was found with that id.
	DetachNotificationRecipient(id string) (removed bool)

	// DetachAllNotificationRecipients must immediately expunge all registered recipients, returning the count of those
	// detached
	DetachAllNotificationRecipients() int
}

type notifierBase struct {
	mu sync.RWMutex
	m  map[string]NotificationHandler
}

func newNotifierBase() *notifierBase {
	nb := new(notifierBase)
	nb.m = make(map[string]NotificationHandler)
	return nb
}

// BasicNotifier is the base implementation of a Notifier
type BasicNotifier struct {
	*notifierBase
}

// NewBasicNotifier returns a new Notifier bereft of any recipients
func NewBasicNotifier() *BasicNotifier {
	b := new(BasicNotifier)
	b.notifierBase = newNotifierBase()
	return b
}

// Push will immediately send a new notification to all currently registered recipients
func (bn *BasicNotifier) Push(s NotificationSource, ev NotificationEvent, d interface{}) {
	bn.sendNotification(s, ev, d)
}

// AttachNotificationHandler immediately adds the provided handler to the list of handlers to be called per notification
func (nb *notifierBase) AttachNotificationHandler(id string, fn NotificationHandler) (string, bool) {
	if fn == nil {
		panic(fmt.Sprintf("AttachNotificationHandler called with id %q and nil handler", id))
	}
	nb.mu.Lock()
	if id == "" {
		id = LazyRandomString(12)
	}
	_, replaced := nb.m[id]
	nb.m[id] = fn
	nb.mu.Unlock()
	return id, replaced
}

// AttachNotificationChannel is a convenience method that will push new notifications onto the provided channel as they
// come in
func (nb *notifierBase) AttachNotificationChannel(id string, ch NotificationChannel) (string, bool) {
	if ch == nil {
		panic(fmt.Sprintf("AttachNotificationChannel called with id %q and nil channel", id))
	}
	return nb.AttachNotificationHandler(id, func(n Notification) {
		ch <- n
	})
}

// DetachNotificationRecipient immediately removes the provided recipient from receiving any new notifications,
// returning true if a recipient was found with the provided id
func (nb *notifierBase) DetachNotificationRecipient(id string) bool {
	var ok bool
	nb.mu.Lock()
	if _, ok = nb.m[id]; ok {
		delete(nb.m, id)
	}
	nb.mu.Unlock()
	return ok
}

// DetachAllNotificationRecipients immediately clears all attached recipients, returning the count of those previously
// attached.
func (nb *notifierBase) DetachAllNotificationRecipients() int {
	nb.mu.Lock()
	cnt := len(nb.m)
	nb.m = make(map[string]NotificationHandler)
	nb.mu.Unlock()
	return cnt
}

// sendNotification immediately calls each handler with the new notification
// TODO: handle cases were fn blocks forever?
func (nb *notifierBase) sendNotification(s NotificationSource, ev NotificationEvent, d interface{}) {
	n := Notification{
		ID:         LazyRandomString(64),
		Originated: time.Now().UnixNano(),
		Source:     s,
		Event:      ev,
		Data:       d,
	}
	nb.mu.RLock()
	for _, fn := range nb.m {
		fn(n)
	}
	nb.mu.RUnlock()
}
