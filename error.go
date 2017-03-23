package consultant

import "fmt"

type WatchPlanStatusCode int

const (
	WatchPlanStatusSuccess WatchPlanStatusCode = iota
	WatchPlanStatusAlreadyStopped
	WatchPlanStatusKeyNotFound
)

type watchPlanError struct {
	code WatchPlanStatusCode
}

func (e watchPlanError) Error() string {
	switch e.code {
	case WatchPlanStatusSuccess:
		return "Success"
	case WatchPlanStatusAlreadyStopped:
		return "WatchPlan has already been told to stop"
	case WatchPlanStatusKeyNotFound:
		return "Key not found"

	default:
		return fmt.Sprintf("An unknown error occurred: \"%d\"", e.code)
	}
}

func (e watchPlanError) String() string {
	return e.Error()
}

func (e watchPlanError) Success() bool        { return e.code == WatchPlanStatusSuccess }
func (e watchPlanError) AlreadyStopped() bool { return e.code == WatchPlanStatusAlreadyStopped }
func (e watchPlanError) KeyNotFound() bool    { return e.code == WatchPlanStatusKeyNotFound }

var (
	ErrWatchPlanAlreadyStopped error = &watchPlanError{WatchPlanStatusAlreadyStopped}
	ErrWatchPlanKeyNotFound    error = &watchPlanError{WatchPlanStatusKeyNotFound}
)

func getWatchPlanError(code WatchPlanStatusCode) error {
	switch code {
	case WatchPlanStatusSuccess:
		return nil
	case WatchPlanStatusAlreadyStopped:
		return ErrWatchPlanAlreadyStopped
	case WatchPlanStatusKeyNotFound:
		return ErrWatchPlanKeyNotFound

	default:
		return &watchPlanError{code}
	}
}
