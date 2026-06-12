package pushsync

type UploadProgressPhase string

const (
	UploadProgressUploading UploadProgressPhase = "uploading"
	UploadProgressCompleted UploadProgressPhase = "completed"
)

type MetadataProgressPhase string

const (
	MetadataProgressRegistering MetadataProgressPhase = "registering"
	MetadataProgressCompleted   MetadataProgressPhase = "completed"
)

type MetadataPlanSummary struct {
	TotalObjects int
}

type MetadataProgressEvent struct {
	Completed int
	Total     int
	Phase     MetadataProgressPhase
}

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
	OnMetadataPlan(MetadataPlanSummary)
	OnMetadataProgress(MetadataProgressEvent)
	OnUploadPlan(UploadPlanSummary)
	OnUploadProgress(UploadProgressEvent)
}
