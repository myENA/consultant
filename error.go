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
