package network

import "errors"

var (
	ErrObjectNil = errors.New("object list can not be empty")
	// ObjectNil is the historical name. New code should use ErrObjectNil.
	ObjectNil = ErrObjectNil
)
