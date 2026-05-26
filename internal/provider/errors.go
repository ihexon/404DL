package provider

import (
	"errors"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"
)

const (
	ErrorPhaseRequest  = "request"
	ErrorPhaseResponse = "response"
)

type HTTPError struct {
	Provider string
	Phase    string
	Method   string
	URL      string
	Status   int
	Body     string
	Err      error
}

func (e *HTTPError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Err != nil:
		return fmt.Sprintf("%s api request failed: %v", e.Provider, e.Err)
	case e.Status != 0:
		return fmt.Sprintf("%s api returned status %d", e.Provider, e.Status)
	default:
		return fmt.Sprintf("%s api failed", e.Provider)
	}
}

func (e *HTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewRequestError(provider string, req *http.Request, err error) error {
	httpErr := &HTTPError{
		Provider: provider,
		Phase:    ErrorPhaseRequest,
		Err:      err,
	}
	setRequestFields(httpErr, req)
	return httpErr
}

func NewStatusError(provider string, req *http.Request, status int, body string) error {
	httpErr := &HTTPError{
		Provider: provider,
		Phase:    ErrorPhaseResponse,
		Status:   status,
		Body:     body,
	}
	setRequestFields(httpErr, req)
	return httpErr
}

func setRequestFields(httpErr *HTTPError, req *http.Request) {
	if req == nil {
		return
	}
	httpErr.Method = req.Method
	if req.URL != nil {
		httpErr.URL = req.URL.String()
	}
}

func ErrorFields(err error) log.Fields {
	fields := log.Fields{
		"error": err,
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		fields["error_phase"] = "provider"
		fields["cause"] = err.Error()
		fields["cause_type"] = fmt.Sprintf("%T", err)
		return fields
	}

	fields["provider"] = httpErr.Provider
	fields["error_phase"] = httpErr.Phase
	fields["http_method"] = httpErr.Method
	fields["http_url"] = httpErr.URL
	if httpErr.Status != 0 {
		fields["http_status"] = httpErr.Status
	}
	if httpErr.Body != "" {
		fields["response_body"] = httpErr.Body
	}
	if httpErr.Err != nil {
		fields["cause"] = httpErr.Err.Error()
		fields["cause_type"] = fmt.Sprintf("%T", httpErr.Err)
	}
	return fields
}
