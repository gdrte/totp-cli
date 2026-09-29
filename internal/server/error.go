package server

// RequestError is a client-side problem with a request, reported as 400.
type RequestError struct {
	Message string
}

func (e RequestError) Error() string {
	return e.Message
}
