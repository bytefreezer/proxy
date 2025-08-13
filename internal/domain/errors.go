package domain

// NotFound wraps custom 404 not found errors
type NotFound struct {
	Err error
}

func (n NotFound) Error() string {
	return n.Err.Error()
}

// Conflict wraps custom 409 conflict errors
type Conflict struct {
	Err error
}

func (c Conflict) Error() string {
	return c.Err.Error()
}

type RollbackFailed struct {
	Err error
}

func (r RollbackFailed) Error() string {
	return r.Err.Error()
}
