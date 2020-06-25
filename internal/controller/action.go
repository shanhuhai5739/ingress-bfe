package controller

import (
	"io"
)

type Action interface {
	handle(c *BfeController) error
}

type LoadConfigAction struct {
	config io.Reader
}

type AddedAction struct {
	resource interface{}
}

type UpdatedAction struct {
	resource    interface{}
	oldResource interface{}
}

type DeletedAction struct {
	resource interface{}
}
