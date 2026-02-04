package ipcapi

import "time"

type AppSummary struct {
	AppID           string `json:"appID"`
	Name            string `json:"name"`
	ExecutablePath  string `json:"executablePath"`
	LastActivityUTC int64  `json:"lastActivityUTC"`
	SnapshotCount   int    `json:"snapshotCount"`
	TrackingState   string `json:"trackingState"`
	RetentionStatus string `json:"retentionStatus"`
}

type SnapshotMeta struct {
	SnapshotID   string `json:"snapshotID"`
	AppID        string `json:"appID"`
	Timestamp    int64  `json:"timestampUTC"`
	WindowsCount int    `json:"windowsCount"`
	FilesAdded   int    `json:"filesAdded"`
	FilesRemoved int    `json:"filesRemoved"`
}

type SnapshotCreatedEvent struct {
	AppID      string       `json:"appID"`
	Snapshot   SnapshotMeta `json:"snapshot"`
	OccurredAt int64        `json:"occurredAtUTC"`
}

type RestoreProgressEvent struct {
	AppID      string `json:"appID"`
	SnapshotID string `json:"snapshotID"`
	Stage      string `json:"stage"`
	Percent    int    `json:"percent"`
	Message    string `json:"message"`
}

type RestoreErrorEvent struct {
	AppID      string `json:"appID"`
	SnapshotID string `json:"snapshotID"`
	Error      string `json:"error"`
}

type TrackingStateChangedEvent struct {
	AppID  *string `json:"appID,omitempty"`
	State  string  `json:"state"`
	Reason string  `json:"reason,omitempty"`
	AtUTC  int64   `json:"atUTC"`
}

func NowUTC() int64 { return time.Now().UTC().UnixMilli() }
