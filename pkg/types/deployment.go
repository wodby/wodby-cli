package types

import "github.com/pkg/errors"

const (
	AppDeploymentStatusCompleted AppDeploymentStatus = iota + 1
	AppDeploymentStatusAwaiting
	AppDeploymentStatusPending
	AppDeploymentStatusInProgress
	AppDeploymentStatusCanceled
	AppDeploymentStatusErrored
)

type (
	DeploymentFromCIInput struct {
		AppBuildID         ID                        `json:"appBuildId"`
		Services           []*ServiceDeploymentInput `json:"services"`
		SkipPostDeployment bool                      `json:"skipPostDeployment"`
	}
	ServiceDeploymentInput struct {
		Name  string `json:"name"`
		Image string `json:"image"`
		// UnmanagedImage reports an image that was not built FROM the service
		// image. Omitted when false so older backends are unaffected.
		UnmanagedImage bool `json:"unmanagedImage,omitempty"`
		// DockerfilePath is empty when Wodby provided the Dockerfile.
		DockerfilePath string `json:"dockerfilePath,omitempty"`
		// DockerfileHash is the SHA-256 of the Dockerfile that produced the image.
		DockerfileHash string `json:"dockerfileHash,omitempty"`
	}
	AppDeployment struct {
		ID ID `json:"id"`
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
