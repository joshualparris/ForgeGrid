package dispatch

import (
	"context"
	"errors"
)

type Request struct{ TaskID, AttemptID, ProjectID, Repository, TaskBranch string }
type Result struct{ Backend, ExternalID, URL string }
type Backend interface {
	Name() string
	Dispatch(context.Context, Request) (Result, error)
	Cancel(context.Context, string) error
}

type LANBackend struct{}

func (LANBackend) Name() string { return "forgegrid-lan" }
func (LANBackend) Dispatch(context.Context, Request) (Result, error) {
	return Result{}, errors.New("ForgeGrid LAN AI-task backend is not implemented")
}
func (LANBackend) Cancel(context.Context, string) error {
	return errors.New("ForgeGrid LAN AI-task backend is not implemented")
}
