// Package validate carries input errors from services to the API layer,
// which turns them into 400 responses that name the offending field.
package validate

type Error struct {
	Field   string
	Message string
}

func (e *Error) Error() string { return e.Field + ": " + e.Message }

func Fail(field, message string) error { return &Error{Field: field, Message: message} }
