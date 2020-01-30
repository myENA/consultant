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
	// 0 - 127

	NotificationEventManualPush NotificationEvent = 0x0 // sent whenever a manual notification is requested
	NotificationEventTestPush   NotificationEvent = 0xf // used for testing purposes

	// 128 - 255

	NotificationEventManagedSessionRunning    NotificationEvent = 0x80 // sent when session is running
	NotificationEventManagedSessionStopped    NotificationEvent = 0x81 // sent when session is no longer running
	NotificationEventManagedSessionCreate     NotificationEvent = 0x82 // sent after an attempt to create an upstream consul session
	NotificationEventManagedSessionRenew      NotificationEvent = 0x83 // sent after a renew attempt on a previously created upstream consul session
	NotificationEventManagedSessionDestroy    NotificationEvent = 0x84 // sent after a destroy attempt on a previously created upstream consul session
	NotificationEventManagedSessionShutdowned NotificationEvent = 0x85 // sent after the managed session has been closed and must be considered defunct

	// 256 - 383

	NotificationEventCandidateRunning      NotificationEvent = 0x100 // sent when candidate enters running
	NotificationEventCandidateStopped      NotificationEvent = 0x101 // sent when candidate leaves running
	NotificationEventCandidateElected      NotificationEvent = 0x102 // sent when candidate has been "elected"
	NotificationEventCandidateLostElection NotificationEvent = 0x103 // sent when candidate lost election
	NotificationEventCandidateResigned     NotificationEvent = 0x104 // sent when candidate explicitly "resigns"
	NotificationEventCandidateRenew        NotificationEvent = 0x105 // sent when candidate was previously elected and attempts to stay elected
	NotificationEventCandidateShutdowned   NotificationEvent = 0x106 // sent when candidate has been closed and must be considered defunct

	// 384 - 511

	NotificationEventManagedServiceRunning          NotificationEvent = 0x180 // sent when the service has reached its "running" state
	NotificationEventManagedServiceStopped          NotificationEvent = 0x181 // sent when the service is considered defunct
	NotificationEventManagedServiceWatchPlanStarted NotificationEvent = 0x182 // sent when the internal watch plan for the service is running
	NotificationEventManagedServiceWatchPlanStopped NotificationEvent = 0x183 // sent when the internal watch plan for the service has stopped
	NotificationEventManagedServiceRefreshed        NotificationEvent = 0x184 // sent whenever an attempt is made to refresh the local cache of the service.  only successful if Error is nil.
	NotificationEventManagedServiceMissing          NotificationEvent = 0x185 // sent when the upstream service is no longer found
	NotificationEventManagedServiceTagsAdded        NotificationEvent = 0x186 // sent when an add tags attempt is made
	NotificationEventManagedServiceTagsRemoved      NotificationEvent = 0x187 // sent when a remove tags attempt is made
	NotificationEventManagedServiceShutdowned       NotificationEvent = 0x188 // sent when managed service has been closed and must be considered defunct
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
	case NotificationEventManagedSessionShutdowned:
		return "ManagedSessionShutdowned"

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
	case NotificationEventCandidateShutdowned:
		return "CandidateShutdowned"

	case NotificationEventManagedServiceRunning:
		return "ManagedServiceRunning"
	case NotificationEventManagedServiceStopped:
		return "ManagedServiceStopped"
	case NotificationEventManagedServiceWatchPlanStarted:
		return "ManagedServiceWatchPlanStarted"
	case NotificationEventManagedServiceWatchPlanStopped:
		return "ManagedServiceWatchPlanStopped"
	case NotificationEventManagedServiceRefreshed:
		return "ManagedServiceRefreshed"
	case NotificationEventManagedServiceMissing:
		return "ManagedServiceMissing"
	case NotificationEventManagedServiceTagsAdded:
		return "ManagedServiceTagsAdded"
	case NotificationEventManagedServiceTagsRemoved:
		return "ManagedServiceTagsRemoved"
	case NotificationEventManagedServiceShutdowned:
		return "ManagedServiceShutdowned"

	default:
		return "UNKNOWN"
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
type NotificationChannel chan Notification

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
	//
	// if wait is true, this method will block until all handlers have been closed
	DetachAllNotificationRecipients(wait bool) int
}

type NotifierAttachResult struct {
	ID        string
	Overwrote bool
}

type notifierWorker struct {
	mu     sync.RWMutex
	closed bool
	wg     *sync.WaitGroup
	id     string
	in     chan Notification
	out    chan Notification
	fn     NotificationHandler
}

func newNotifierWorker(id string, wg *sync.WaitGroup, fn NotificationHandler) *notifierWorker {
	nw := new(notifierWorker)
	nw.in = make(chan Notification, 100)
	nw.out = make(chan Notification)
	nw.id = id
	nw.wg = wg
	nw.fn = fn
	go nw.publish()
	go nw.process()
	return nw
}

func (nw *notifierWorker) close() {
	nw.mu.Lock()
	nw.closed = true
	nw.mu.Unlock()
}

func (nw *notifierWorker) publish() {
	timer := time.NewTimer(5 * time.Second)

	// queue up func to close then drain ingest channel
	defer func() {
		close(nw.in)
		close(nw.out)
		if len(nw.in) > 0 {
			for range nw.in {
			}
		}
	}()

	for {
		nw.mu.RLock()
		if nw.closed {
			nw.mu.RUnlock()
			return
		}
		nw.mu.RUnlock()

		select {
		case <-timer.C:
			// test state, if closed exit and do not process any more messages
			nw.mu.RLock()
			if nw.closed {
				nw.mu.RUnlock()
				return
			}
			nw.mu.RUnlock()

			timer.Reset(5 * time.Second)

		case n := <-nw.in:
			if !timer.Stop() {
				<-timer.C
			}

			// todo: it is probably not necessary to test here as if the worker is closed between this notification
			// being processed and the next notification in, it is removed from the map of available workers to push
			// to before close == true, meaning it cannot have new messages pushed to it.
			nw.mu.RLock()
			if nw.closed {
				nw.mu.RUnlock()
				return
			}

			// attempt to push message to consumer, allowing for up to 5 seconds of blocking
			// if block window passes, drop on floor
			waitForConsumer := time.NewTimer(5 * time.Second)
			select {
			case nw.out <- n:
				if !waitForConsumer.Stop() {
					<-waitForConsumer.C
				}
			case <-waitForConsumer.C:
			}

			timer.Reset(5 * time.Second)

			nw.mu.RUnlock()
		}
	}
}

func (nw *notifierWorker) process() {
	// nw.out is an unbuffered channel.  it blocks until any preceding notification has been handled by the registered
	// handler.  it is closed once the publish() loop breaks.
	for n := range nw.out {
		nw.fn(n)
	}

	// mark done only after nw.out loop has exited
	nw.wg.Done()
}

func (nw *notifierWorker) push(n Notification) {
	// hold an rlock for the duration of the push attempt to ensure that, at a minimum, the message is added to the
	// channel before it can be closed.
	nw.mu.RLock()
	defer nw.mu.RUnlock()

	if nw.closed {
		return
	}

	// attempt to push message to ingest chan.  if chan is full, drop on floor
	select {
	case nw.in <- n:
	default:
	}
}

type notifierBase struct {
	mu      sync.RWMutex
	workers map[string]*notifierWorker
	hr      chan *notifierWorker
	wg      *sync.WaitGroup
}

func newNotifierBase(log Logger, debug bool) *notifierBase {
	nb := new(notifierBase)
	nb.workers = make(map[string]*notifierWorker)
	nb.wg = new(sync.WaitGroup)
	return nb
}

// BasicNotifier is the base implementation of a Notifier
type BasicNotifier struct {
	*notifierBase
}

// NewBasicNotifier returns the default Notifier implementation
func NewBasicNotifier(log Logger, debug bool) *BasicNotifier {
	b := new(BasicNotifier)
	b.notifierBase = newNotifierBase(log, debug)
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
	var (
		w        *notifierWorker
		replaced bool
	)

	nb.mu.Lock()
	defer nb.mu.Unlock()

	nb.wg.Add(1)

	if id == "" {
		id = LazyRandomString(12)
	}
	w, replaced = nb.workers[id]

	nb.workers[id] = newNotifierWorker(id, nb.wg, fn)
	if replaced {
		w.close()
	}
	return id, replaced
}

// AttachNotificationHandlers allows you to attach 1 or more notification handlers at a time
func (nb *notifierBase) AttachNotificationHandlers(fns ...NotificationHandler) []NotifierAttachResult {
	l := len(fns)
	if l == 0 {
		return nil
	}

	results := make([]NotifierAttachResult, l, l)
	for i, fn := range fns {
		res := new(NotifierAttachResult)
		res.ID, res.Overwrote = nb.AttachNotificationHandler("", fn)
		results[i] = *res
	}

	return results
}

// AttachNotificationChannel will register a new channel for notifications to be pushed to
func (nb *notifierBase) AttachNotificationChannel(id string, ch NotificationChannel) (string, bool) {
	if ch == nil {
		panic(fmt.Sprintf("AttachNotificationChannel called with id %q and nil channel", id))
	}
	return nb.AttachNotificationHandler(id, func(n Notification) {
		ch <- n
	})
}

// AttachNotificationChannels will attempt to attach multiple channels at once
func (nb *notifierBase) AttachNotificationChannels(chs ...NotificationChannel) []NotifierAttachResult {
	l := len(chs)
	if l == 0 {
		return nil
	}

	results := make([]NotifierAttachResult, l, l)
	for i, ch := range chs {
		res := new(NotifierAttachResult)
		res.ID, res.Overwrote = nb.AttachNotificationChannel("", ch)
		results[i] = *res
	}

	return results
}

// DetachNotificationRecipient immediately removes the provided recipient from receiving any new notifications,
// returning true if a recipient was found with the provided id
func (nb *notifierBase) DetachNotificationRecipient(id string) bool {
	var (
		w  *notifierWorker
		ok bool
	)

	nb.mu.Lock()
	defer nb.mu.Unlock()

	if w, ok = nb.workers[id]; ok {
		w.close()
	}
	delete(nb.workers, id)

	return ok
}

// DetachAllNotificationRecipients immediately clears all attached recipients, returning the count of those previously
// attached.
func (nb *notifierBase) DetachAllNotificationRecipients(wait bool) int {
	nb.mu.Lock()

	cnt := len(nb.workers)
	current := nb.workers
	nb.workers = make(map[string]*notifierWorker)

	nb.mu.Unlock()

	go func() {
		for _, w := range current {
			w.close()
		}
	}()

	if wait {
		nb.wg.Wait()
	}

	return cnt
}

// sendNotification immediately calls each handler with the new notification
func (nb *notifierBase) sendNotification(s NotificationSource, ev NotificationEvent, d interface{}) {
	n := Notification{
		ID:         LazyRandomString(64),
		Originated: time.Now().UnixNano(),
		Source:     s,
		Event:      ev,
		Data:       d,
	}
	nb.mu.RLock()
	for _, w := range nb.workers {
		w.push(n)
	}
	nb.mu.RUnlock()
}
