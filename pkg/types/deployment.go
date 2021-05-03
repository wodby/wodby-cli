package types

import (
	"database/sql"
	"time"

	"github.com/pkg/errors"
)

const (
	AppDeploymentStatusCompleted AppDeploymentStatus = iota + 1
	AppDeploymentStatusAwaiting
	AppDeploymentStatusPending
	AppDeploymentStatusInProgress
	AppDeploymentStatusCanceled
	AppDeploymentStatusErrored
)

type (
	DeploymentInput struct {
		AppBuildID     int                       `json:"appBuildID"`
		Services       []*ServiceDeploymentInput `json:"services"`
		PostDeployment bool                      `json:"postDeployment"`
	}
	ServiceDeploymentInput struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}
	AppDeployment struct {
		ID            int
		AppInstanceID int `db:"app_instance_id"`
		AppBuildID    int `db:"app_build_id"`
		TaskID        int `db:"task_id"`
		Status        AppDeploymentStatus
		CreatedAt     time.Time    `db:"created_at"`
		UpdatedAt     time.Time    `db:"updated_at"`
		StartedAt     sql.NullTime `db:"started_at"`
		EndedAt       sql.NullTime `db:"ended_at"`
	}
	AppDeploymentStatus int
)

func (s AppDeploymentStatus) IsCompleted() bool {
	return s == AppDeploymentStatusCompleted
}

func (s AppDeploymentStatus) String() string {
	return map[AppDeploymentStatus]string{
		AppDeploymentStatusCompleted:  "completed",
		AppDeploymentStatusAwaiting:   "awaiting",
		AppDeploymentStatusPending:    "pending",
		AppDeploymentStatusInProgress: "in_progress",
		AppDeploymentStatusCanceled:   "canceled",
		AppDeploymentStatusErrored:    "errored",
	}[s]
}

func (s *AppDeploymentStatus) Scan(src interface{}) error {
	v, ok := src.(string)
	if !ok {
		return errors.Errorf("invalid app deployment status format: %v", src)
	}
	switch v {
	case AppDeploymentStatusCompleted.String():
		*s = AppDeploymentStatusCompleted
	case AppDeploymentStatusAwaiting.String():
		*s = AppDeploymentStatusAwaiting
	case AppDeploymentStatusPending.String():
		*s = AppDeploymentStatusPending
	case AppDeploymentStatusInProgress.String():
		*s = AppDeploymentStatusInProgress
	case AppDeploymentStatusCanceled.String():
		*s = AppDeploymentStatusCanceled
	case AppDeploymentStatusErrored.String():
		*s = AppDeploymentStatusErrored
	default:
		return errors.Errorf("unexpected app deployment status: %v", src)
	}
	return nil
}