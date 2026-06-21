package domain

import "errors"

var (
	ErrDriverNotFound = errors.New("Driver not found")
	ErrDriverAlreadyExists = errors.New("Driver already exists")
	ErrDriverBusy = errors.New("Driver is busy")
)