package store

import "errors"

var (
	ErrCreativeInvalid   = errors.New("invalid creative input")
	ErrCreativeConflict  = errors.New("creative transition conflict")
	ErrCreativeForbidden = errors.New("creative action requires human approval")
)

type CreativeStageDefinition struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	ExpectedArtifacts string `json:"expected_artifacts"`
	Repeated          bool   `json:"repeated"`
	Approval          string `json:"approval"` // auto | stage | item
}
type CreativePipelineInput struct {
	Name   string                    `json:"name"`
	Stages []CreativeStageDefinition `json:"stages"`
}
type CreativePipeline struct {
	ID int64 `json:"id"`
	CreativePipelineInput
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}
type CreativeRunInput struct {
	ProjectID  string `json:"project_id"`
	PipelineID int64  `json:"pipeline_id"`
	Name       string `json:"name"`
	Brief      string `json:"brief"`
}
type CreativeRun struct {
	ID int64 `json:"id"`
	CreativeRunInput
	AllowedProfiles       []string        `json:"allowed_profiles"`
	PipelineName          string          `json:"pipeline_name"`
	OrchestratorSessionID string          `json:"orchestrator_session_id"`
	Status                string          `json:"status"`
	ClosureState          string          `json:"closure_state"`
	AcceptanceRequestedAt int64           `json:"acceptance_requested_at"`
	AcceptedAt            int64           `json:"accepted_at"`
	ClosureError          string          `json:"closure_error"`
	Stages                []CreativeStage `json:"stages"`
	CreatedAt             int64           `json:"created_at"`
	UpdatedAt             int64           `json:"updated_at"`
}
type CreativeProgress struct {
	Progress     int    `json:"progress"`
	ProgressNote string `json:"progress_note"`
}
type CreativeReview struct {
	ReviewRevision     int64   `json:"review_revision"`
	ArtifactVersionIDs []int64 `json:"artifact_version_ids"`
}
type CreativeStage struct {
	ID       int64 `json:"id"`
	RunID    int64 `json:"run_id"`
	Position int   `json:"position"`
	CreativeStageDefinition
	Status string `json:"status"`
	CreativeProgress
	CreativeReview
	Items []CreativeItem `json:"items"`
}
type CreativeItemInput struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	ExpectedArtifacts string `json:"expected_artifacts"`
}
type CreativeItem struct {
	ID       int64 `json:"id"`
	RunID    int64 `json:"run_id"`
	StageID  int64 `json:"stage_id"`
	Position int   `json:"position"`
	CreativeItemInput
	Status string `json:"status"`
	CreativeProgress
	CreativeReview
}
type CreativeContext struct {
	RunID                 int64  `json:"run_id"`
	StageID               int64  `json:"stage_id,omitempty"`
	ItemID                int64  `json:"item_id,omitempty"`
	ProjectID             string `json:"project_id"`
	OrchestratorSessionID string `json:"orchestrator_session_id"`
}
type CreativeAction struct {
	Reason             string  `json:"reason,omitempty"`               // required for reject/withdraw
	Action             string  `json:"action"`                         // start | submit | approve | reject | reopen | withdraw
	ReviewRevision     int64   `json:"review_revision,omitempty"`      // required for approve/reject/withdraw
	ArtifactVersionIDs []int64 `json:"artifact_version_ids,omitempty"` // submit snapshot
}
