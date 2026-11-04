export type Approval = 'auto' | 'stage' | 'item'
export type Status = 'pending' | 'in_progress' | 'awaiting_approval' | 'completed'
export interface StageDefinition { name: string; description: string; expected_artifacts: string; repeated: boolean; approval: Approval }
export interface Pipeline { id: number; project_id: string; name: string; stages: StageDefinition[] }
export interface Work { id: number; name: string; description: string; expected_artifacts: string; status: Status; progress: number; progress_note: string; review_revision: number; artifact_version_ids: number[] }
export interface Item extends Work { stage_id: number; run_id: number; position: number }
export interface Stage extends Work, StageDefinition { run_id: number; position: number; items: Item[] }
export interface Run { id: number; project_id: string; pipeline_id: number; pipeline_name: string; name: string; brief: string; status: Status; orchestrator_session_id: string; stages: Stage[] }
export interface Scope { run_id: number; stage_id?: number; item_id?: number }
export interface Artifact { id: number; run_id: number; stage_id?: number; item_id?: number; name: string; kind: 'source' | 'result'; current_version_id: number }
export interface ArtifactVersion { id: number; artifact_id: number; version_number: number; filename: string; mime: string; size: number; content_url: string; download_url: string }
export interface ArtifactDetail { artifact: Artifact; versions: ArtifactVersion[] }
export interface ShownVersion { artifact: Artifact; version: ArtifactVersion }
export interface Context extends Scope { label: string; artifact_version_ids: number[] }
export interface PreviewAccess extends Scope { id: string; port: number; base_path: string; status: 'ready' | 'unavailable'; url?: string; message?: string }
