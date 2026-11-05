import type { Session } from "../../web/src/lib/types";

export type Approval = "auto" | "stage" | "item";
export type Status =
  | "pending"
  | "in_progress"
  | "awaiting_approval"
  | "completed";
export interface StageDefinition {
  name: string;
  description: string;
  expected_artifacts: string;
  repeated: boolean;
  approval: Approval;
}
export interface Pipeline {
  id: number;
  name: string;
  stages: StageDefinition[];
}
export interface Work {
  id: number;
  name: string;
  description: string;
  expected_artifacts: string;
  status: Status;
  progress: number;
  progress_note: string;
  review_revision: number;
  artifact_version_ids: number[];
}
export interface Item extends Work {
  stage_id: number;
  run_id: number;
  position: number;
}
export interface Stage extends Work, StageDefinition {
  run_id: number;
  position: number;
  items: Item[];
}
export interface Run {
  id: number;
  project_id: string;
  pipeline_id: number;
  pipeline_name: string;
  name: string;
  brief: string;
  status: Status;
  orchestrator_session_id: string;
  stages: Stage[];
}
export interface Scope {
  run_id: number;
  stage_id?: number;
  item_id?: number;
}
export interface Artifact {
  id: number;
  run_id: number;
  stage_id?: number;
  item_id?: number;
  name: string;
  kind: "source" | "result";
  current_version_id: number;
}
export interface ArtifactVersion {
  id: number;
  artifact_id: number;
  version_number: number;
  filename: string;
  mime: string;
  size: number;
  content_url: string;
  download_url: string;
}
export interface ArtifactDetail {
  artifact: Artifact;
  versions: ArtifactVersion[];
}
export interface ShownVersion {
  item_name?: string;
  artifact: Artifact;
  version: ArtifactVersion;
}
export interface Context extends Scope {
  label: string;
  artifact_version_ids: number[];
}
export interface PreviewAccess extends Scope {
  id: string;
  port: number;
  base_path: string;
  status: "ready" | "unavailable";
  url?: string;
  message?: string;
}
export interface CreativeSession extends Session {
  creative_context?: Scope;
}
export interface CreativeMessage {
  id: number;
  from?: string;
  to: string;
  body: string;
  status: string;
  created_at: number;
  reason?: string;
  creative_context: Scope;
  artifact_version_ids: number[];
}
export interface CreativeQuestion {
  id: number;
  title: string;
  body: string;
  status: string;
  resolution?: string;
  options?: string[];
  your_turn: boolean;
  local_ref: string;
  ordinal?: number;
  asked_at?: number;
  brief?: string;
  stale?: boolean;
  asked_by: string;
  creative_context: Scope;
  artifact_version_ids: number[];
  messages: { id: number; author: string; body: string; created_at: number }[];
}
