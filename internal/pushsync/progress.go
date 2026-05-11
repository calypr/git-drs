package pushsync

type UploadProgressPhase string

const (
	UploadProgressUploading UploadProgressPhase = "uploading"
	UploadProgressCompleted UploadProgressPhase = "completed"
)

type UploadPlanFile struct {
	OID   string
	Path  string
	Bytes int64
}

type UploadPlanSummary struct {
	Files      []UploadPlanFile
	TotalFiles int
	TotalBytes int64
}

type UploadProgressEvent struct {
	OID            string
	Path           string
	BytesSoFar     int64
	BytesSinceLast int64
	TotalBytes     int64
	Phase          UploadProgressPhase
}

type UploadProgressReporter interface {
	OnUploadPlan(UploadPlanSummary)
	OnUploadProgress(UploadProgressEvent)
}
