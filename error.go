package consultant

import "fmt"

type WatchPlanErrorCode int

const (
	WatchPlanErrorNone WatchPlanErrorCode = iota
	WatchPlanErrorAlreadyStopped
	WatchPlanErrorKeyNotFound
)

type watchPlanError struct {
	code WatchPlanErrorCode
}

func (e watchPlanError) Error() string {
	switch e.code {
	case WatchPlanErrorNone:
		return "No Error"
	case WatchPlanErrorAlreadyStopped:
		return "WatchPlan has already been told to stop"
	case WatchPlanErrorKeyNotFound:
		return "Key not found"

	default:
		return fmt.Sprintf("An unknown error occurred: \"%d\"", e.code)
	}
}

func (e watchPlanError) String() string           { return e.Error() }
func (e watchPlanError) Code() WatchPlanErrorCode { return e.code }

func (e watchPlanError) None() bool           { return e.code == WatchPlanErrorNone }
func (e watchPlanError) AlreadyStopped() bool { return e.code == WatchPlanErrorAlreadyStopped }
func (e watchPlanError) KeyNotFound() bool    { return e.code == WatchPlanErrorKeyNotFound }

var (
	ErrWatchPlanAlreadyStopped error = &watchPlanError{WatchPlanErrorAlreadyStopped}
	ErrWatchPlanKeyNotFound    error = &watchPlanError{WatchPlanErrorKeyNotFound}
)

func getWatchPlanError(code WatchPlanErrorCode) error {
	switch code {
	case WatchPlanErrorNone:
		return nil
	case WatchPlanErrorAlreadyStopped:
		return ErrWatchPlanAlreadyStopped
	case WatchPlanErrorKeyNotFound:
		return ErrWatchPlanKeyNotFound

	default:
		return &watchPlanError{code}
	}
}

type CandidateErrorCode int

const (
	CandidateErrorNone CandidateErrorCode = iota
	CandidateErrorInvalidID
	CandidateErrorInvalidTTL
	CandidateErrorKeyNotFound
	CandidateErrorNoSession
)

type candidateError struct {
	code CandidateErrorCode
}

func (e candidateError) Error() string {
	switch e.code {
	case CandidateErrorNone:
		return "No Error"
	case CandidateErrorInvalidID:
		return candidateIDErrMsg
	case CandidateErrorInvalidTTL:
		return "Invalid TTL specified"
	case CandidateErrorKeyNotFound:
		return "Unable to locate KV key"
	case CandidateErrorNoSession:
		return "No session associated with KV key"

	default:
		return fmt.Sprintf("An unknown error occurred: \"%d\"", e.code)
	}
}

func (e candidateError) String() string           { return e.Error() }
func (e candidateError) Code() CandidateErrorCode { return e.code }

func (e candidateError) None() bool            { return e.code == CandidateErrorNone }
func (e candidateError) InvalidID() bool       { return e.code == CandidateErrorInvalidID }
func (e candidateError) InvalidTTL() bool      { return e.code == CandidateErrorInvalidTTL }
func (e candidateError) KeyNotFound() bool     { return e.code == CandidateErrorKeyNotFound }
func (e candidateError) SessionNotFound() bool { return e.code == CandidateErrorNoSession }

var (
	ErrCandidateInvalidID   error = &candidateError{CandidateErrorInvalidID}
	ErrCandidateInvalidTTL  error = &candidateError{CandidateErrorInvalidTTL}
	ErrCandidateKeyNotFound error = &candidateError{CandidateErrorKeyNotFound}
	ErrCandidateNoSession   error = &candidateError{CandidateErrorNoSession}
)

func getCandidateError(code CandidateErrorCode) error {
	switch code {
	case CandidateErrorNone:
		return nil
	case CandidateErrorInvalidID:
		return ErrCandidateInvalidID
	case CandidateErrorInvalidTTL:
		return ErrCandidateInvalidTTL
	case CandidateErrorKeyNotFound:
		return ErrCandidateKeyNotFound
	case CandidateErrorNoSession:
		return ErrCandidateNoSession

	default:
		return &candidateError{code}
	}
}

type SiblingLocatorErrorCode int

const (
	SiblingLocatorErrorNone SiblingLocatorErrorCode = iota
	SiblingLocatorErrorNameEmpty
	SiblingLocatorErrorLocalIDEmpty
	SiblingLocatorErrorWatcherAlreadyRunning
	SiblingLocatorErrorWatcherCreateFailed
	SiblingLocatorErrorCurrentCallFailed
)

type siblingLocatorError struct {
	code SiblingLocatorErrorCode
}

func (e siblingLocatorError) Error() string {
	switch e.code {
	case SiblingLocatorErrorNone:
		return "None"
	case SiblingLocatorErrorNameEmpty:
		return "Service name cannot be empty"
	case SiblingLocatorErrorLocalIDEmpty:
		return "Local Service ID cannot be empty"
	case SiblingLocatorErrorWatcherAlreadyRunning:
		return "Service Sibling watcher is already running"
	case SiblingLocatorErrorWatcherCreateFailed:
		return "Unable to construct service WatchPlan"
	case SiblingLocatorErrorCurrentCallFailed:
		return "Unable to locate current siblings"

	default:
		return fmt.Sprintf("An unknown error occurred: \"%d\"", e.code)
	}
}

func (e siblingLocatorError) String() string                { return e.Error() }
func (e siblingLocatorError) Code() SiblingLocatorErrorCode { return e.code }

func (e siblingLocatorError) None() bool         { return e.code == SiblingLocatorErrorNone }
func (e siblingLocatorError) NameEmpty() bool    { return e.code == SiblingLocatorErrorNameEmpty }
func (e siblingLocatorError) LocalIDEmpty() bool { return e.code == SiblingLocatorErrorLocalIDEmpty }
func (e siblingLocatorError) WatcherAlreadyRunning() bool {
	return e.code == SiblingLocatorErrorWatcherAlreadyRunning
}
func (e siblingLocatorError) WatcherCreateFailed() bool {
	return e.code == SiblingLocatorErrorWatcherCreateFailed
}
func (e siblingLocatorError) CurrentCallFailed() bool {
	return e.code == SiblingLocatorErrorCurrentCallFailed
}

var (
	ErrSiblingLocatorNameEmpty           error = &siblingLocatorError{SiblingLocatorErrorNameEmpty}
	ErrSiblingLocatorLocalIDEmpty        error = &siblingLocatorError{SiblingLocatorErrorLocalIDEmpty}
	ErrSiblingLocatorAlreadyRunning      error = &siblingLocatorError{SiblingLocatorErrorWatcherAlreadyRunning}
	ErrSiblingLocatorWatcherCreateFailed error = &siblingLocatorError{SiblingLocatorErrorWatcherCreateFailed}
	ErrSiblingLocatorCurrentCallFailed   error = &siblingLocatorError{SiblingLocatorErrorCurrentCallFailed}
)

func getSiblingLocatorError(code SiblingLocatorErrorCode) error {
	switch code {
	case SiblingLocatorErrorNameEmpty:
		return ErrSiblingLocatorNameEmpty
	case SiblingLocatorErrorLocalIDEmpty:
		return ErrSiblingLocatorLocalIDEmpty
	case SiblingLocatorErrorWatcherAlreadyRunning:
		return ErrSiblingLocatorAlreadyRunning
	case SiblingLocatorErrorWatcherCreateFailed:
		return ErrSiblingLocatorWatcherCreateFailed
	case SiblingLocatorErrorCurrentCallFailed:
		return ErrSiblingLocatorCurrentCallFailed

	default:
		return &siblingLocatorError{code}
	}
}
