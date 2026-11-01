export type MessageRole = "user" | "assistant";

export interface MessageAttachment {
  name: string;
  size: number;
  data_url?: string;
  mime_type?: string;
  generated?: boolean;
}

export interface ChatUploadLimits {
  max_files: number;
  max_file_size_mib: number;
  max_total_size_mib: number;
}

export interface ChatMessage {
  id: string;
  role: MessageRole;
  content: string;
  attachments?: MessageAttachment[];
  /** Original browser files are retained only for retry during this session. */
  attachmentFiles?: File[];
  /** True when the message has an ID assigned by the API and exists in storage. */
  persisted?: boolean;
  createdAt: Date;
  isStreaming?: boolean;
  researchSteps?: ResearchProgress[];
  sources?: ChatSource[];
  generatedImages?: string[];
}

export interface ChatSource {
  type: "institutional" | "web";
  title: string;
  source_reference?: string;
  url?: string;
}

export interface ResearchProgress {
  step: number;
  max_steps: number;
  action: "planning" | "search_web" | "read_page" | "execute_python" | "synthesis" | "fallback";
  input?: string;
  status: "started" | "completed" | "blocked" | "failed" | "empty" | "fallback";
  message: string;
  sources?: Array<{ title: string; url: string }>;
}

export interface ChatRequest {
  prompt: string;
  conversation_id: string;
  model?: string;
}

export interface StreamChunk {
  text?: string;
  error?: string;
  conversation_id?: string;
  message_id?: string;
  title?: string;
  step?: number;
  max_steps?: number;
  action?: ResearchProgress["action"];
  input?: string;
  status?: ResearchProgress["status"];
  message?: string;
  sources?: ChatSource[];
  data_url?: string;
}

export interface PersistedConversation {
  id: string;
  title: string;
  folder_id: string | null;
  archived_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ConversationFolder {
  id: string;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface PersistedMessage {
  id: string;
  conversation_id: string;
  role: MessageRole;
  content: string;
  attachments?: MessageAttachment[];
  created_at: string;
}
