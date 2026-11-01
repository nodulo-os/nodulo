"use client";

import { useCallback, useEffect, useRef, useState, type ChangeEvent, type DragEvent } from "react";
import Link from "next/link";
import {
  Activity, AlertCircle, AlertTriangle, ArrowDownToLine, ArrowLeft, Atom, BookOpen, Building2,
  CheckCircle2, ChevronLeft, ChevronRight, CircleHelp, CloudDownload, Database,
  ChevronDown, FileText, FolderArchive, Gauge, Globe, HardDrive, LoaderCircle, LogOut, Mail, MessageSquareText, Paperclip,
  RefreshCw, Search, Server, ShieldCheck, Trash2, Upload, Users, X,
} from "lucide-react";
import { ProtectedPage } from "@/components/auth/ProtectedPage";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { useAuth } from "@/context/AuthContext";
import { ThemeToggleButton } from "@/context/ThemeContext";
import { apiURL } from "@/lib/api";
import { apiErrorMessage, userFacingError } from "@/lib/errors";

type Tab = "overview" | "users" | "access" | "identity" | "uploads" | "models" | "knowledge" | "webSearch" | "observability";
type Stats = {
  database_size: string;
  embeddings_size: string;
  total_users: number;
  total_conversations: number;
  total_messages: number;
  total_chunks: number;
};
type ModelInfo = { name: string; size: number };
type DocumentInfo = {
  title: string;
  source_reference: string;
  chunks_count: number;
  last_ingested_at: string;
};
type AdminUser = { id: string; email: string; name: string; role: "admin" | "user"; registration_status: "pending" | "approved" | "rejected"; rejection_reason?: string; created_at: string };
type RegistrationSettings = { mode: "automatic" | "approval"; allowed_domains: string[]; updated_at: string };
type AuthorizedEmail = { email: string; note: string; created_at: string };
type InstitutionalSettings = { show_institution: boolean; institution_name: string; institution_acronym: string; institution_unit: string; updated_at?: string };
type ChatUploadSettings = { max_files: number; max_file_size_mib: number; max_total_size_mib: number; updated_at?: string };
type WebSearchSettings = { enabled: boolean; provider: "duckduckgo" | "searxng"; endpoint: string; result_limit: number; updated_at?: string };
type PullProgress = { status?: string; total?: number; completed?: number };
type IngestionJob = {
  id: string;
  filename: string;
  file_type: "pdf" | "zip" | "url" | "text" | "docx" | "pptx";
  source_reference: string;
  status: "pending" | "processing" | "completed" | "failed";
  total_chunks: number;
  processed_chunks: number;
  progress_percent?: number;
  error_message?: string;
  created_at: string;
  updated_at: string;
};
type AdminConfirmation = { title: string; description: string; confirmLabel: string; action: () => Promise<void> };
type ObservabilityData = {
  metrics: { started_at: string; uptime_seconds: number; in_flight: number; total_requests: number; status_counts: Record<string, number>; routes: { route: string; count: number; errors: number; average_duration_ms: number }[]; requests_per_minute: { requests: number; errors: number }[]; recent_audit: { time: string; event: string; request_id?: string; actor_user_id?: string }[]; dropped_errors: number; log_persistence_failures: number };
  process: { memory_alloc_bytes: number; memory_sys_bytes: number; goroutines: number; gc_count: number };
  dependencies: Record<string, { status: string; latency_ms: number; available_models?: number; checked_at: string }>;
};
type ErrorLog = { id: number; occurred_at: string; level: "WARN" | "ERROR"; source: string; message: string; details?: string; request_id?: string; actor_user_id?: string; method?: string; route?: string; status_code?: number };
type ErrorLogPage = { items: ErrorLog[]; has_more: boolean; next_cursor?: number };

const tabs: { id: Tab; label: string; icon: typeof Gauge }[] = [
  { id: "overview", label: "Visão geral", icon: Gauge },
  { id: "users", label: "Utilizadores", icon: Users },
  { id: "access", label: "Acesso e cadastros", icon: ShieldCheck },
  { id: "identity", label: "Identidade da instalação", icon: Building2 },
  { id: "uploads", label: "Anexos do chat", icon: Paperclip },
  { id: "models", label: "Modelos LLM", icon: Atom },
  { id: "knowledge", label: "Base de conhecimento", icon: BookOpen },
  { id: "webSearch", label: "Pesquisa web", icon: Globe },
  { id: "observability", label: "Monitoramento", icon: Activity },
];

const maxBatchBytes = 200 * 1024 * 1024;
const maxBatchFiles = 20;
const documentPageSize = 8;

async function readError(response: Response) {
  return apiErrorMessage(response, "Não foi possível concluir a solicitação. Verifique sua conexão e tente novamente.");
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** index).toLocaleString("pt-BR", { maximumFractionDigits: index ? 1 : 0 })} ${units[index]}`;
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : new Intl.DateTimeFormat("pt-BR", { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function JobCard({ job, onRetry, retrying }: { job: IngestionJob; onRetry: (job: IngestionJob, file: File) => void; retrying: boolean }) {
  const retryInput = useRef<HTMLInputElement>(null);
  const visibleError = job.error_message
    ? userFacingError(job.error_message, "O processamento foi interrompido. Tente continuar com o arquivo original ou avise o administrador.")
    : null;
  const progress = job.status === "completed"
    ? 100
    : job.total_chunks > 0
      ? Math.min(100, Math.round(job.processed_chunks / job.total_chunks * 100))
      : job.progress_percent || 0;
  const status = {
    pending: { label: "Pendente", color: "border-amber-300/20 bg-amber-300/[0.07] text-amber-200", icon: AlertTriangle },
    processing: { label: "Processando", color: "border-sky-300/20 bg-sky-300/[0.07] text-sky-200", icon: LoaderCircle },
    completed: { label: "Concluído", color: "border-emerald-300/20 bg-emerald-300/[0.07] text-emerald-200", icon: CheckCircle2 },
    failed: { label: "Falha", color: "border-rose-300/20 bg-rose-300/[0.07] text-rose-200", icon: AlertTriangle },
  }[job.status];
  const Icon = job.file_type === "url" ? Globe : job.file_type === "zip" ? FolderArchive : FileText;
  const StatusIcon = status.icon;

  return (
    <article className="rounded-xl border border-white/[0.07] bg-black/15 p-4">
      <div className="flex items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-white/[0.045] text-emerald-200"><Icon size={16} /></span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0"><p className="truncate text-sm font-medium text-zinc-200" title={job.filename}>{job.filename}</p><p className="mt-1 truncate text-[11px] text-zinc-500" title={job.source_reference}>{job.source_reference}</p></div>
            <span className={`inline-flex shrink-0 items-center gap-1.5 rounded-full border px-2 py-1 text-[10px] font-medium ${status.color}`}><StatusIcon size={12} className={job.status === "processing" ? "animate-spin" : ""} />{status.label}</span>
          </div>
          <div className="mt-3 flex items-center justify-between gap-3 text-[11px] text-zinc-500"><span>{job.processed_chunks.toLocaleString("pt-BR")} / {job.total_chunks.toLocaleString("pt-BR")} fragmentos</span><span className="font-mono text-zinc-400">{progress}%</span></div>
          <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-white/[0.07]"><div className={`h-full rounded-full transition-all duration-500 ${job.status === "failed" ? "bg-rose-400" : job.status === "completed" ? "bg-emerald-300" : job.status === "pending" ? "bg-amber-300" : "bg-sky-300"} ${job.status === "processing" && job.total_chunks === 0 ? "w-1/4 animate-pulse" : ""}`} style={job.status === "processing" && job.total_chunks === 0 ? undefined : { width: `${progress}%` }} /></div>
          <div className="mt-2 flex items-center justify-between gap-3 text-[10px] text-zinc-600"><span>{job.status === "processing" ? "Vetores sendo gerados" : `Atualizado ${formatDate(job.updated_at)}`}</span><span className="shrink-0 uppercase">{job.file_type}</span></div>
          {job.status === "failed" && <>
            {visibleError && <p role="alert" className="mt-2 rounded-lg border border-rose-300/15 bg-rose-300/[0.05] px-2.5 py-2 text-xs leading-5 text-rose-200">{visibleError}</p>}
            {job.file_type !== "url" && <div className="mt-3">
              <input ref={retryInput} type="file" accept=".pdf,.txt,.md,.zip,.docx,.pptx" className="sr-only" aria-label={`Selecionar novamente ${job.filename}`} onChange={(event) => {
                const file = event.target.files?.[0];
                if (file) onRetry(job, file);
                event.target.value = "";
              }} />
              <button type="button" onClick={() => retryInput.current?.click()} disabled={retrying} className="inline-flex min-h-9 items-center gap-2 rounded-lg border border-emerald-300/20 bg-emerald-300/[0.07] px-3 text-xs font-medium text-emerald-100 transition hover:bg-emerald-300/[0.12] disabled:cursor-wait disabled:opacity-60">
                {retrying ? <LoaderCircle size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                {retrying ? "Retomando…" : "Selecionar arquivo para continuar"}
              </button>
              <p className="mt-1.5 text-[10px] leading-4 text-zinc-500">Escolha o mesmo arquivo original; os fragmentos já indexados serão verificados e preservados.</p>
            </div>}
          </>}
        </div>
      </div>
    </article>
  );
}

function AdminContent() {
  const { user, logout, authorizedFetch } = useAuth();
  const [tab, setTab] = useState<Tab>("overview");
  const [stats, setStats] = useState<Stats | null>(null);
  const [observabilityData, setObservabilityData] = useState<ObservabilityData | null>(null);
  const [observabilityError, setObservabilityError] = useState("");
  const [observabilityLoading, setObservabilityLoading] = useState(false);
  const [errorLogs, setErrorLogs] = useState<ErrorLog[]>([]);
  const [errorLogsLoading, setErrorLogsLoading] = useState(false);
  const [errorLogsError, setErrorLogsError] = useState("");
  const [errorLogQuery, setErrorLogQuery] = useState("");
  const [errorLogLevel, setErrorLogLevel] = useState("");
  const [errorLogsCursor, setErrorLogsCursor] = useState<number | undefined>();
  const [errorLogsHasMore, setErrorLogsHasMore] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [documents, setDocuments] = useState<DocumentInfo[]>([]);
  const [adminUsers, setAdminUsers] = useState<AdminUser[]>([]);
  const [registrationSettings, setRegistrationSettings] = useState<RegistrationSettings | null>(null);
  const [institutionalSettings, setInstitutionalSettings] = useState<InstitutionalSettings | null>(null);
  const [institutionalBusy, setInstitutionalBusy] = useState(false);
  const [institutionalFeedback, setInstitutionalFeedback] = useState("");
  const [chatUploadSettings, setChatUploadSettings] = useState<ChatUploadSettings | null>(null);
  const [chatUploadBusy, setChatUploadBusy] = useState(false);
  const [chatUploadFeedback, setChatUploadFeedback] = useState("");
  const [webSearchSettings, setWebSearchSettings] = useState<WebSearchSettings | null>(null);
  const [webSearchBusy, setWebSearchBusy] = useState(false);
  const [webSearchFeedback, setWebSearchFeedback] = useState("");
  const [authorizedEmails, setAuthorizedEmails] = useState<AuthorizedEmail[]>([]);
  const [notificationConfigured, setNotificationConfigured] = useState(false);
  const [allowedDomainsText, setAllowedDomainsText] = useState("");
  const [authorizedEmailsText, setAuthorizedEmailsText] = useState("");
  const [allowlistNote, setAllowlistNote] = useState("");
  const [rejectionReasons, setRejectionReasons] = useState<Record<string, string>>({});
  const [accessBusy, setAccessBusy] = useState(false);
  const [reviewingUserID, setReviewingUserID] = useState("");
  const [resendingUserID, setResendingUserID] = useState("");
  const [usersFeedback, setUsersFeedback] = useState("");
  const [accessFeedback, setAccessFeedback] = useState("");
  const [loadingStats, setLoadingStats] = useState(true);
  const [loadingModels, setLoadingModels] = useState(true);
  const [loadingDocuments, setLoadingDocuments] = useState(true);
  const [loadingUsers, setLoadingUsers] = useState(true);
  const [updatingUserID, setUpdatingUserID] = useState("");
  const [deletingUserID, setDeletingUserID] = useState("");
  const [providerError, setProviderError] = useState("");
  const [pageError, setPageError] = useState("");
  const [confirmation, setConfirmation] = useState<AdminConfirmation | null>(null);
  const [confirmationBusy, setConfirmationBusy] = useState(false);
  const [modelName, setModelName] = useState("");
  const [pulling, setPulling] = useState(false);
  const [pullStatus, setPullStatus] = useState("");
  const [pullPercent, setPullPercent] = useState<number | null>(null);
  const [deletingModel, setDeletingModel] = useState("");
  const [knowledgeMode, setKnowledgeMode] = useState<"files" | "web">("files");
  const [fileTitle, setFileTitle] = useState("");
  const [fileReference, setFileReference] = useState("");
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const [draggingFiles, setDraggingFiles] = useState(false);
  const [webURL, setWebURL] = useState("");
  const [webTitle, setWebTitle] = useState("");
  const [webReference, setWebReference] = useState("");
  const [ingestionBusy, setIngestionBusy] = useState(false);
  const [ingestionFeedback, setIngestionFeedback] = useState("");
  const [jobs, setJobs] = useState<IngestionJob[]>([]);
  const [jobsLoading, setJobsLoading] = useState(true);
  const [jobsError, setJobsError] = useState("");
  const [retryingJobID, setRetryingJobID] = useState("");
  const [jobsNotice, setJobsNotice] = useState("");
  const [documentSearch, setDocumentSearch] = useState("");
  const [documentPage, setDocumentPage] = useState(1);
  const [deletingSource, setDeletingSource] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  const pullAbort = useRef<AbortController | null>(null);
  const hasActiveJobs = useRef(false);

  const fetchStats = useCallback(async () => {
    setLoadingStats(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/stats"));
      if (!response.ok) throw new Error(await readError(response));
      setStats(await response.json() as Stats);
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível carregar as métricas. Verifique a conexão com a API e tente novamente."));
    } finally { setLoadingStats(false); }
  }, [authorizedFetch]);

  const fetchObservability = useCallback(async (signal?: AbortSignal) => {
    setObservabilityLoading(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/observability"), { signal });
      if (!response.ok) throw new Error(await readError(response));
      setObservabilityData(await response.json() as ObservabilityData);
      setObservabilityError("");
    } catch (error) {
      if (signal?.aborted) return;
      setObservabilityError(userFacingError(error, "Não foi possível carregar os dados de monitoramento."));
    } finally { if (!signal?.aborted) setObservabilityLoading(false); }
  }, [authorizedFetch]);

  const fetchErrorLogs = useCallback(async (append = false, signal?: AbortSignal, beforeID?: number) => {
    setErrorLogsLoading(true);
    const params = new URLSearchParams({ limit: "50" });
    if (errorLogQuery.trim()) params.set("q", errorLogQuery.trim());
    if (errorLogLevel) params.set("level", errorLogLevel);
    if (append && beforeID) params.set("before_id", String(beforeID));
    try {
      const response = await authorizedFetch(apiURL(`/api/admin/observability/errors?${params.toString()}`), { signal });
      if (!response.ok) throw new Error(await readError(response));
      const page = await response.json() as ErrorLogPage;
      setErrorLogs((current) => append ? [...current, ...page.items] : page.items);
      setErrorLogsCursor(page.next_cursor);
      setErrorLogsHasMore(page.has_more);
      setErrorLogsError("");
    } catch (error) {
      if (signal?.aborted) return;
      setErrorLogsError(userFacingError(error, "Não foi possível carregar o histórico de erros."));
    } finally { if (!signal?.aborted) setErrorLogsLoading(false); }
  }, [authorizedFetch, errorLogLevel, errorLogQuery]);

  const fetchModels = useCallback(async () => {
    setLoadingModels(true);
    setProviderError("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/models"));
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as ModelInfo[];
      setModels(Array.isArray(result) ? result : []);
    } catch (error) {
      setProviderError(userFacingError(error, "Não foi possível consultar o serviço de IA configurado. Verifique a conexão e tente novamente."));
    } finally { setLoadingModels(false); }
  }, [authorizedFetch]);

  const fetchDocuments = useCallback(async () => {
    setLoadingDocuments(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/documents"));
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as DocumentInfo[];
      setDocuments(Array.isArray(result) ? result : []);
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível carregar o acervo. Verifique a conexão com o banco e tente novamente."));
    } finally { setLoadingDocuments(false); }
  }, [authorizedFetch]);

  const fetchUsers = useCallback(async () => {
    setLoadingUsers(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/users"));
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as AdminUser[];
      setAdminUsers(Array.isArray(result) ? result : []);
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível carregar os utilizadores. Verifique a conexão com a API e tente novamente."));
    } finally { setLoadingUsers(false); }
  }, [authorizedFetch]);

  const fetchRegistrationSettings = useCallback(async () => {
    setAccessBusy(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/registration/settings"));
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as { settings: RegistrationSettings; authorized_emails: AuthorizedEmail[]; notification_configured: boolean };
      setRegistrationSettings(result.settings);
      setAllowedDomainsText(result.settings.allowed_domains.join("\n"));
      setAuthorizedEmails(result.authorized_emails || []);
      setNotificationConfigured(result.notification_configured);
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível carregar as regras de cadastro. Tente novamente ou avise o administrador."));
    } finally { setAccessBusy(false); }
  }, [authorizedFetch]);

  const fetchInstitutionalSettings = useCallback(async () => {
    setInstitutionalBusy(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/site-settings"));
      if (!response.ok) throw new Error(await readError(response));
      setInstitutionalSettings(await response.json() as InstitutionalSettings);
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível carregar as informações da instalação. Tente novamente."));
    } finally { setInstitutionalBusy(false); }
  }, [authorizedFetch]);

  const fetchChatUploadSettings = useCallback(async () => {
    setChatUploadBusy(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/chat-upload-settings"));
      if (!response.ok) throw new Error(await readError(response));
      setChatUploadSettings(await response.json() as ChatUploadSettings);
    } catch (error) {
      setChatUploadFeedback(userFacingError(error, "Não foi possível carregar os limites de anexos."));
    } finally { setChatUploadBusy(false); }
  }, [authorizedFetch]);

  const fetchWebSearchSettings = useCallback(async () => {
    setWebSearchBusy(true);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/web-search-settings"));
      if (!response.ok) throw new Error(await readError(response));
      setWebSearchSettings(await response.json() as WebSearchSettings);
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível carregar as configurações de pesquisa web."));
    } finally { setWebSearchBusy(false); }
  }, [authorizedFetch]);

  const applyJobs = useCallback((result: IngestionJob[]) => {
    const nextJobs = Array.isArray(result) ? result : [];
    const hadActiveJobs = hasActiveJobs.current;
    const hasActive = nextJobs.some((job) => job.status === "pending" || job.status === "processing");
    setJobs(nextJobs);
    hasActiveJobs.current = hasActive;
    if (hadActiveJobs && !hasActive) {
      void fetchDocuments();
      void fetchStats();
    }
  }, [fetchDocuments, fetchStats]);

  const fetchJobs = useCallback(async (showLoading = false) => {
    if (showLoading) setJobsLoading(true);
    setJobsError("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/jobs"));
      if (!response.ok) throw new Error(await readError(response));
      applyJobs(await response.json() as IngestionJob[]);
    } catch (error) {
      setJobsError(userFacingError(error, "Não foi possível consultar os processamentos. Verifique a conexão com a API e tente novamente."));
    } finally { if (showLoading) setJobsLoading(false); }
  }, [applyJobs, authorizedFetch]);

  const retryJob = useCallback(async (job: IngestionJob, file: File) => {
    if (file.name !== job.filename) {
      setJobsError(`Selecione o arquivo original: ${job.filename}.`);
      return;
    }
    setRetryingJobID(job.id);
    setJobsError("");
    setJobsNotice("");
    const form = new FormData();
    form.append("file", file);
    try {
      const response = await authorizedFetch(apiURL(`/api/admin/jobs/${encodeURIComponent(job.id)}/retry`), {
        method: "POST",
        body: form,
      });
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as { processed_chunks?: number };
      await fetchJobs();
      const progress = result.processed_chunks ?? job.processed_chunks;
      setJobsNotice(`Arquivo recebido. O processamento foi retomado a partir dos ${progress.toLocaleString("pt-BR")} fragmentos já confirmados.`);
    } catch (error) {
      setJobsError(userFacingError(error, "Não foi possível retomar o processamento. Confira o arquivo e tente novamente."));
    } finally {
      setRetryingJobID("");
    }
  }, [authorizedFetch, fetchJobs]);

  useEffect(() => {
    void fetchStats();
    void fetchModels();
    void fetchDocuments();
    void fetchUsers();
  }, [fetchStats, fetchModels, fetchDocuments, fetchUsers]);

  useEffect(() => () => pullAbort.current?.abort(), []);

  useEffect(() => {
    if (tab !== "knowledge") return;
    void fetchJobs(true);
    const controller = new AbortController();
    let reconnectTimer: number | undefined;

    const waitToReconnect = () => new Promise<void>((resolve) => {
      reconnectTimer = window.setTimeout(resolve, 2000);
    });

    const consumeFrame = (frame: string) => {
      let eventName = "message";
      const data: string[] = [];
      for (const line of frame.split(/\r?\n/)) {
        if (line.startsWith("event:")) eventName = line.slice(6).trim();
        else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
      }
      if (!data.length) return;
      const payload = data.join("\n");
      if (eventName === "error") {
        let message = "O servidor não conseguiu atualizar o monitor de ingestão.";
        try { message = (JSON.parse(payload) as { error?: string }).error || message; } catch { /* mantém mensagem legível */ }
        throw new Error(message);
      }
      if (eventName !== "jobs") return;
      const parsed = JSON.parse(payload) as IngestionJob[];
      applyJobs(parsed);
      setJobsError("");
      setJobsLoading(false);
    };

    const streamJobs = async () => {
      while (!controller.signal.aborted) {
        let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
        try {
          const response = await authorizedFetch(apiURL("/api/admin/jobs/stream"), {
            headers: { Accept: "text/event-stream" },
            signal: controller.signal,
          });
          if (!response.ok) throw new Error(await readError(response));
          if (!response.body) throw new Error("O servidor não disponibilizou o stream de jobs.");
          reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = "";
          setJobsError("");
          while (!controller.signal.aborted) {
            const { value, done } = await reader.read();
            buffer += decoder.decode(value, { stream: !done });
            const frames = buffer.split(/\r?\n\r?\n/);
            buffer = frames.pop() || "";
            for (const frame of frames) consumeFrame(frame);
            if (done) break;
          }
          if (buffer.trim()) consumeFrame(buffer);
          if (!controller.signal.aborted) throw new Error("A conexão do monitor foi encerrada.");
        } catch (error) {
          if (controller.signal.aborted) return;
          await reader?.cancel().catch(() => undefined);
          setJobsError(`${userFacingError(error, "A conexão com o monitor foi interrompida.")} Tentando novamente…`);
          // Mantém o painel utilizável enquanto o SSE reconecta.
          await fetchJobs();
          await waitToReconnect();
        }
      }
    };

    void streamJobs();
    return () => {
      controller.abort();
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
    };
  }, [applyJobs, authorizedFetch, fetchJobs, tab]);

  useEffect(() => {
    if (tab === "access") {
      void fetchRegistrationSettings();
      void fetchUsers();
    }
  }, [fetchRegistrationSettings, fetchUsers, tab]);

  useEffect(() => {
    if (tab === "identity") void fetchInstitutionalSettings();
  }, [fetchInstitutionalSettings, tab]);

  useEffect(() => {
    if (tab === "uploads") void fetchChatUploadSettings();
  }, [fetchChatUploadSettings, tab]);

  useEffect(() => {
    if (tab === "webSearch") void fetchWebSearchSettings();
  }, [fetchWebSearchSettings, tab]);

  useEffect(() => {
    if (tab !== "observability") return;
    const controller = new AbortController();
    void fetchObservability(controller.signal);
    void fetchErrorLogs(false, controller.signal);
    const timer = window.setInterval(() => {
      void fetchObservability(controller.signal);
      void fetchErrorLogs(false, controller.signal);
    }, 10000);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, [fetchErrorLogs, fetchObservability, tab]);

  useEffect(() => { setDocumentPage(1); }, [documentSearch]);

  const handlePull = async () => {
    const requestedModel = modelName.trim();
    if (!requestedModel || pulling) return;
    setPageError(""); setPullStatus("Conectando ao provedor de IA…"); setPullPercent(null); setPulling(true);
    const controller = new AbortController();
    pullAbort.current = controller;
    try {
      const response = await authorizedFetch(apiURL("/api/admin/models/pull"), {
        method: "POST", headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
        body: JSON.stringify({ model: requestedModel }), signal: controller.signal,
      });
      if (!response.ok) throw new Error(await readError(response));
      if (!response.body) throw new Error("O servidor não forneceu um stream de progresso.");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let completed = false;
      const processFrame = (frame: string) => {
        let event = "message";
        const data: string[] = [];
        for (const line of frame.split(/\r?\n/)) {
          if (line.startsWith("event:")) event = line.slice(6).trim();
          else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
        }
        if (!data.length) return;
        const payload = data.join("\n");
        if (payload === "[DONE]") { completed = true; return; }
        if (event === "error") {
          try { throw new Error((JSON.parse(payload) as { error?: string }).error || "Falha ao descarregar o modelo."); }
          catch (error) { if (error instanceof SyntaxError) throw new Error(payload); throw error; }
        }
        if (event !== "progress") return;
        const progress = JSON.parse(payload) as PullProgress;
        if (progress.status) setPullStatus(progress.status);
        if (typeof progress.total === "number" && progress.total > 0 && typeof progress.completed === "number") {
          setPullPercent(Math.min(100, Math.round(progress.completed / progress.total * 100)));
        }
      };
      while (true) {
        const { value, done } = await reader.read();
        buffer += decoder.decode(value, { stream: !done });
        const frames = buffer.split(/\r?\n\r?\n/);
        buffer = frames.pop() || "";
        for (const frame of frames) processFrame(frame);
        if (done || completed) break;
      }
      if (buffer.trim()) processFrame(buffer);
      if (completed) {
        setPullPercent(100); setPullStatus(`Modelo ${requestedModel} pronto.`); setModelName("");
        await fetchModels();
      }
    } catch (error) {
      if (!(error instanceof DOMException && error.name === "AbortError")) {
        setPageError(userFacingError(error, "Não foi possível obter o modelo. Verifique a conexão com o provedor de IA e tente novamente."));
      }
    } finally { setPulling(false); pullAbort.current = null; }
  };

  const handleDeleteModel = async (name: string) => {
    setDeletingModel(name); setPageError("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/models"), {
        method: "DELETE", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ model: name }),
      });
      if (!response.ok) throw new Error(await readError(response));
      await fetchModels();
    } catch (error) { setPageError(userFacingError(error, "Não foi possível remover o modelo. Tente novamente.")); }
    finally { setDeletingModel(""); }
  };

  const chooseFiles = (candidates: FileList | File[]) => {
    const incoming = Array.from(candidates);
    if (incoming.length === 0) return;
    if (incoming.length > maxBatchFiles) {
      setIngestionFeedback(`Selecione no máximo ${maxBatchFiles} arquivos por lote.`);
      setSelectedFiles([]);
      return;
    }
    const supported = incoming.every((candidate) => ["pdf", "txt", "md", "zip"].includes(candidate.name.toLowerCase().split(".").pop() || ""));
    if (!supported) {
      setIngestionFeedback("Formatos aceitos: .pdf, .txt, .md e .zip.");
      setSelectedFiles([]);
      return;
    }
    if (incoming.some((candidate) => candidate.size > maxBatchBytes)) {
      setIngestionFeedback("Cada arquivo pode ter no máximo 200 MB.");
      setSelectedFiles([]);
      return;
    }
    if (incoming.reduce((total, candidate) => total + candidate.size, 0) > maxBatchBytes) {
      setIngestionFeedback("O tamanho total do lote não pode ultrapassar 200 MB.");
      setSelectedFiles([]);
      return;
    }
    setIngestionFeedback("");
    setSelectedFiles(incoming);
  };

  const handleFileChange = (event: ChangeEvent<HTMLInputElement>) => chooseFiles(event.target.files || []);
  const handleDrop = (event: DragEvent<HTMLButtonElement>) => {
    event.preventDefault(); setDraggingFiles(false); chooseFiles(event.dataTransfer.files);
  };

  const handleUploadBatch = async () => {
    if (selectedFiles.length === 0 || !fileReference.trim() || ingestionBusy) {
      setIngestionFeedback("Selecione um arquivo e informe a disciplina ou referência geral."); return;
    }
    setIngestionBusy(true); setIngestionFeedback(""); setPageError("");
    const form = new FormData();
    selectedFiles.forEach((selected) => form.append("files", selected));
    form.set("source_reference", fileReference.trim());
    if (selectedFiles.length === 1 && fileTitle.trim()) form.set("title", fileTitle.trim());
    try {
      const response = await authorizedFetch(apiURL("/api/admin/documents/upload"), { method: "POST", body: form });
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as { created_count?: number };
      const count = result.created_count ?? selectedFiles.length;
      setIngestionFeedback(`${count} ${count === 1 ? "job enviado" : "jobs enviados"} para processamento. Acompanhe o progresso abaixo.`);
      setSelectedFiles([]); setFileTitle(""); setFileReference("");
      if (fileInput.current) fileInput.current.value = "";
      hasActiveJobs.current = true;
      await Promise.all([fetchJobs(), fetchDocuments(), fetchStats()]);
    } catch (error) {
      setIngestionFeedback(userFacingError(error, "Não foi possível enviar os arquivos. Verifique os formatos e tamanhos e tente novamente."));
    } finally { setIngestionBusy(false); }
  };

  const handleScrapeURL = async () => {
    if (!webURL.trim() || !webTitle.trim() || !webReference.trim() || ingestionBusy) {
      setIngestionFeedback("Informe a URL, o título e a referência institucional."); return;
    }
    let parsedURL: URL;
    try { parsedURL = new URL(webURL.trim()); }
    catch { setIngestionFeedback("Informe uma URL válida iniciada por https:// ou http://."); return; }
    if (parsedURL.protocol !== "https:" && parsedURL.protocol !== "http:") {
      setIngestionFeedback("A fonte deve usar HTTP ou HTTPS."); return;
    }
    setIngestionBusy(true); setIngestionFeedback(""); setPageError("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/documents/scrape"), {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: parsedURL.toString(), title: webTitle.trim(), source_reference: webReference.trim() }),
      });
      if (!response.ok) throw new Error(await readError(response));
      setIngestionFeedback("Fonte adicionada à fila. O conteúdo será capturado e indexado em segundo plano.");
      setWebURL(""); setWebTitle(""); setWebReference("");
      hasActiveJobs.current = true;
      await fetchJobs();
    } catch (error) {
      setIngestionFeedback(userFacingError(error, "Não foi possível adicionar a página à fila. Confira o endereço e tente novamente."));
    } finally { setIngestionBusy(false); }
  };

  const handleDeleteDocument = async (document: DocumentInfo) => {
    setDeletingSource(document.source_reference); setPageError("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/documents"), {
        method: "DELETE", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ source_reference: document.source_reference }),
      });
      if (!response.ok) throw new Error(await readError(response));
      await Promise.all([fetchDocuments(), fetchStats()]);
    } catch (error) { setPageError(userFacingError(error, "Não foi possível remover o documento. Tente novamente.")); }
    finally { setDeletingSource(""); }
  };

  const handleUserRoleChange = async (target: AdminUser, role: AdminUser["role"]) => {
    if (target.role === role) return;
    setUpdatingUserID(target.id); setPageError("");
    try {
      const response = await authorizedFetch(apiURL(`/api/admin/users/${target.id}`), {
        method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ role }),
      });
      if (!response.ok) throw new Error(await readError(response));
      const updated = await response.json() as AdminUser;
      setAdminUsers((current) => current.map((item) => item.id === updated.id ? updated : item));
    } catch (error) {
      setPageError(userFacingError(error, "Não foi possível alterar o perfil de acesso. Atualize a lista e tente novamente."));
      void fetchUsers();
    } finally { setUpdatingUserID(""); }
  };

  const handleDeleteUser = async (target: AdminUser) => {
    if (target.id === user?.id) return;
    setDeletingUserID(target.id); setPageError("");
    try {
      const response = await authorizedFetch(apiURL(`/api/admin/users/${target.id}`), { method: "DELETE" });
      if (!response.ok) throw new Error(await readError(response));
      setAdminUsers((current) => current.filter((item) => item.id !== target.id));
      void fetchStats();
    } catch (error) { setPageError(userFacingError(error, "Não foi possível remover a conta. Verifique se ela ainda está ativa e tente novamente.")); }
    finally { setDeletingUserID(""); }
  };

  const runConfirmation = async () => {
    if (!confirmation || confirmationBusy) return;
    setConfirmationBusy(true);
    try {
      await confirmation.action();
      setConfirmation(null);
    } finally {
      setConfirmationBusy(false);
    }
  };

  const saveRegistrationSettings = async () => {
    if (!registrationSettings || accessBusy) return;
    setAccessBusy(true); setAccessFeedback(""); setPageError("");
    const allowedDomains = allowedDomainsText.split(/[\n,;]/).map((domain) => domain.trim().replace(/^@/, "").toLowerCase()).filter(Boolean);
    try {
      const response = await authorizedFetch(apiURL("/api/admin/registration/settings"), {
        method: "PATCH", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: registrationSettings.mode, allowed_domains: allowedDomains }),
      });
      if (!response.ok) throw new Error(await readError(response));
      const updated = await response.json() as RegistrationSettings;
      setRegistrationSettings(updated);
      setAllowedDomainsText(updated.allowed_domains.join("\n"));
      setAccessFeedback("Política de cadastro salva.");
    } catch (error) { setAccessFeedback(userFacingError(error, "Não foi possível salvar a política de cadastro. Confira os dados e tente novamente.")); }
    finally { setAccessBusy(false); }
  };

  const addAuthorizedEmails = async () => {
    const emails = authorizedEmailsText.split(/[\n,;\s]+/).map((email) => email.trim().toLowerCase()).filter(Boolean);
    if (emails.length === 0 || accessBusy) { setAccessFeedback("Informe um ou mais e-mails para autorizar."); return; }
    setAccessBusy(true); setAccessFeedback(""); setPageError("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/registration/allowlist"), {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ emails, note: allowlistNote.trim() }),
      });
      if (!response.ok) throw new Error(await readError(response));
      setAuthorizedEmails(await response.json() as AuthorizedEmail[]);
      setAuthorizedEmailsText(""); setAllowlistNote("");
      setAccessFeedback(`${emails.length} e-mail(s) autorizado(s).`);
    } catch (error) { setAccessFeedback(userFacingError(error, "Não foi possível atualizar a lista de e-mails autorizados. Confira os dados e tente novamente.")); }
    finally { setAccessBusy(false); }
  };

  const deleteAuthorizedEmail = async (email: string) => {
    if (accessBusy) return;
    setAccessBusy(true); setAccessFeedback("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/registration/allowlist"), {
        method: "DELETE", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email }),
      });
      if (!response.ok) throw new Error(await readError(response));
      setAuthorizedEmails((current) => current.filter((item) => item.email !== email));
      setAccessFeedback(`A autorização de ${email} foi removida.`);
    } catch (error) { setAccessFeedback(userFacingError(error, "Não foi possível remover esta autorização. Atualize a lista e tente novamente.")); }
    finally { setAccessBusy(false); }
  };

  const reviewRegistration = async (target: AdminUser, decision: "approved" | "rejected") => {
    const reason = rejectionReasons[target.id]?.trim() || "";
    if (decision === "rejected" && !reason) { setAccessFeedback("Escreva uma justificativa antes de rejeitar o cadastro."); return; }
    setReviewingUserID(target.id); setAccessFeedback(""); setPageError("");
    try {
      const response = await authorizedFetch(apiURL(`/api/admin/users/${target.id}/review`), {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: decision, reason }),
      });
      if (!response.ok) throw new Error(await readError(response));
      const result = await response.json() as { user: AdminUser; notification_sent: boolean };
      setAdminUsers((current) => current.map((item) => item.id === result.user.id ? result.user : item));
      setRejectionReasons((current) => { const next = { ...current }; delete next[target.id]; return next; });
      setAccessFeedback(result.notification_sent
        ? `Cadastro ${decision === "approved" ? "aprovado" : "rejeitado"}; o e-mail de decisão foi enviado.`
        : `Decisão registrada, mas o e-mail não foi enviado. ${notificationConfigured ? "Verifique a configuração do servidor SMTP." : "Configure SMTP_HOST, SMTP_PORT e SMTP_FROM no ambiente da API."}`);
      await fetchUsers();
    } catch (error) { setAccessFeedback(userFacingError(error, "Não foi possível registrar a decisão sobre o cadastro. Tente novamente.")); }
    finally { setReviewingUserID(""); }
  };

  const resendRegistrationNotification = async (target: AdminUser) => {
    setResendingUserID(target.id); setUsersFeedback("");
    try {
      const response = await authorizedFetch(apiURL(`/api/admin/users/${target.id}/notification`), { method: "POST" });
      if (!response.ok) throw new Error(await readError(response));
      setUsersFeedback(`Notificação reenviada para ${target.email}.`);
    } catch (error) {
      setUsersFeedback(userFacingError(error, "Não foi possível reenviar o e-mail. Verifique a configuração de notificações e tente novamente."));
    } finally { setResendingUserID(""); }
  };

  const saveInstitutionalSettings = async () => {
    if (!institutionalSettings || institutionalBusy) return;
    setInstitutionalBusy(true); setInstitutionalFeedback("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/site-settings"), {
        method: "PATCH", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(institutionalSettings),
      });
      if (!response.ok) throw new Error(await readError(response));
      setInstitutionalSettings(await response.json() as InstitutionalSettings);
      setInstitutionalFeedback("Informações da instalação salvas.");
    } catch (error) {
      setInstitutionalFeedback(userFacingError(error, "Não foi possível salvar as informações da instalação. Confira os campos e tente novamente."));
    } finally { setInstitutionalBusy(false); }
  };

  const saveChatUploadSettings = async () => {
    if (!chatUploadSettings || chatUploadBusy) return;
    setChatUploadBusy(true); setChatUploadFeedback("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/chat-upload-settings"), {
        method: "PATCH", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          max_files: chatUploadSettings.max_files,
          max_file_size_mib: chatUploadSettings.max_file_size_mib,
          max_total_size_mib: chatUploadSettings.max_total_size_mib,
        }),
      });
      if (!response.ok) throw new Error(await readError(response));
      setChatUploadSettings(await response.json() as ChatUploadSettings);
      setChatUploadFeedback("Limites de anexos salvos. Novos envios já usarão estas configurações.");
    } catch (error) {
      setChatUploadFeedback(userFacingError(error, "Não foi possível salvar os limites de anexos."));
    } finally { setChatUploadBusy(false); }
  };

  const saveWebSearchSettings = async () => {
    if (!webSearchSettings || webSearchBusy) return;
    setWebSearchBusy(true); setWebSearchFeedback("");
    try {
      const response = await authorizedFetch(apiURL("/api/admin/web-search-settings"), {
        method: "PATCH", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(webSearchSettings),
      });
      if (!response.ok) throw new Error(await readError(response));
      setWebSearchSettings(await response.json() as WebSearchSettings);
      setWebSearchFeedback("Configurações de pesquisa web salvas e aplicadas aos próximos chats.");
    } catch (error) {
      setWebSearchFeedback(userFacingError(error, "Não foi possível salvar a configuração de pesquisa web."));
    } finally { setWebSearchBusy(false); }
  };

  const connected = !loadingModels && !providerError;
  const filteredDocuments = documents.filter((document) => {
    const needle = documentSearch.trim().toLocaleLowerCase();
    return !needle || document.title.toLocaleLowerCase().includes(needle) || document.source_reference.toLocaleLowerCase().includes(needle);
  });
  const documentPageCount = Math.max(1, Math.ceil(filteredDocuments.length / documentPageSize));
  const visibleDocuments = filteredDocuments.slice((documentPage - 1) * documentPageSize, documentPage * documentPageSize);
  const totalSelectedBytes = selectedFiles.reduce((total, selected) => total + selected.size, 0);
  const activeJobsCount = jobs.filter((job) => job.status === "pending" || job.status === "processing").length;

  return (
    <main className="theme-adaptive min-h-screen bg-[#0a0d0c] text-zinc-100">
      <header className="sticky top-0 z-20 border-b border-white/[0.07] bg-[#0a0d0c]/90 backdrop-blur-xl">
        <div className="mx-auto flex max-w-[1440px] items-center justify-between gap-4 px-4 py-3 sm:px-7 lg:px-10">
          <div className="flex min-w-0 items-center gap-3">
            <Link href="/chat" aria-label="Voltar ao chat" className="grid size-9 shrink-0 place-items-center rounded-lg border border-white/10 text-zinc-400 transition hover:border-emerald-300/30 hover:text-emerald-200"><ArrowLeft size={17} /></Link>
            <span className="grid size-9 shrink-0 place-items-center rounded-xl border border-emerald-300/20 bg-emerald-300/[0.08] text-emerald-200"><Atom size={19} /></span>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="text-sm font-semibold tracking-[.12em]">NÓDULO <span className="text-zinc-600">/</span> ADMIN</span>
                <span className="hidden rounded-full border border-emerald-300/15 bg-emerald-300/[0.06] px-2 py-0.5 text-[10px] font-medium tracking-wide text-emerald-200 sm:inline-flex">AMBIENTE DE GOVERNANÇA</span>
              </div>
              <p className="truncate text-xs text-zinc-500">Olá, {user?.name || "administrador"}</p>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <span className={`hidden items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] sm:inline-flex ${loadingModels ? "border-white/10 text-zinc-400" : connected ? "border-emerald-300/15 text-emerald-200" : "border-rose-300/15 text-rose-200"}`}>
              <span className={`size-1.5 rounded-full ${loadingModels ? "bg-zinc-500" : connected ? "bg-emerald-400" : "bg-rose-400"}`} />{loadingModels ? "Verificando" : connected ? "Provedor de IA disponível" : "Provedor de IA indisponível"}
            </span>
            <ThemeToggleButton />
            <Link href="/chat" className="admin-chat-link" aria-label="Abrir chat">
              <MessageSquareText size={15} aria-hidden="true" />
              <span className="hidden sm:inline">Abrir chat</span>
              <span className="sm:hidden">Chat</span>
            </Link>
            <button onClick={logout} className="inline-flex items-center gap-2 rounded-lg border border-white/10 px-3 py-2 text-sm text-zinc-300 transition hover:border-white/20 hover:bg-white/[0.04]"><LogOut size={15} /><span className="hidden sm:inline">Sair</span></button>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-[1440px] px-4 pb-12 pt-7 sm:px-7 sm:pt-10 lg:px-10">
        <div className="mb-7 flex flex-col justify-between gap-5 sm:flex-row sm:items-end">
          <div><div className="mb-3 inline-flex items-center gap-2 text-xs font-medium uppercase tracking-[.17em] text-emerald-200/80"><ShieldCheck size={14} /> Administração institucional</div><h1 className="text-3xl font-medium tracking-[-.04em] sm:text-4xl">Painel de governança</h1><p className="mt-2 max-w-2xl text-sm leading-6 text-zinc-400">Acompanhe a infraestrutura de IA, gerencie modelos locais e mantenha o acervo académico atualizado.</p></div>
          <button onClick={() => { void fetchStats(); void fetchModels(); void fetchDocuments(); void fetchUsers(); if (tab === "access") void fetchRegistrationSettings(); if (tab === "identity") void fetchInstitutionalSettings(); if (tab === "uploads") void fetchChatUploadSettings(); if (tab === "webSearch") void fetchWebSearchSettings(); if (tab === "knowledge") void fetchJobs(true); if (tab === "observability") { void fetchObservability(); void fetchErrorLogs(); } }} className="inline-flex h-10 items-center justify-center gap-2 self-start rounded-lg border border-white/10 px-3 text-sm text-zinc-300 transition hover:bg-white/[0.05] sm:self-auto"><RefreshCw size={14} className={tab === "observability" && observabilityLoading ? "animate-spin" : ""} /> Atualizar dados</button>
        </div>

        {pageError && <div role="alert" className="mb-5 flex items-start gap-2 rounded-xl border border-rose-300/15 bg-rose-300/[0.06] px-4 py-3 text-sm text-rose-100"><AlertCircle size={17} className="mt-0.5 shrink-0" /><span className="min-w-0 flex-1">{pageError}</span><button onClick={() => setPageError("")} aria-label="Fechar aviso"><X size={16} /></button></div>}

        <nav aria-label="Secções administrativas" className="mb-7 grid grid-cols-2 gap-1 rounded-xl border border-white/[0.07] bg-white/[0.025] p-1 sm:grid-cols-3 sm:gap-1.5 lg:grid-cols-4 2xl:grid-cols-7">
          {tabs.map(({ id, label, icon: Icon }) => <button key={id} onClick={() => setTab(id)} aria-current={tab === id ? "page" : undefined} className={`flex min-h-12 min-w-0 items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs outline-none transition focus-visible:ring-2 focus-visible:ring-emerald-400 focus-visible:ring-offset-2 focus-visible:ring-offset-[#101513] sm:min-h-11 sm:px-3 sm:text-sm ${tab === id ? "bg-emerald-300/[0.12] text-emerald-100 shadow-sm" : "text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200"}`}><Icon size={15} className="shrink-0" /><span className="min-w-0 flex-1 whitespace-normal leading-tight">{label}</span>{id === "users" && <span className="shrink-0 rounded-full bg-white/[0.07] px-1.5 py-0.5 text-[10px] text-zinc-300">{stats?.total_users ?? "…"}</span>}{id === "access" && <span className="shrink-0 rounded-full bg-amber-300/[0.1] px-1.5 py-0.5 text-[10px] text-amber-200">{adminUsers.filter((account) => account.registration_status === "pending").length}</span>}</button>)}
        </nav>

        {tab === "observability" && <ObservabilityPanel data={observabilityData} error={observabilityError} loading={observabilityLoading} logs={errorLogs} logsLoading={errorLogsLoading} logsError={errorLogsError} logQuery={errorLogQuery} onLogQueryChange={setErrorLogQuery} logLevel={errorLogLevel} onLogLevelChange={setErrorLogLevel} onRefreshLogs={() => void fetchErrorLogs()} hasMoreLogs={errorLogsHasMore} onLoadMoreLogs={() => void fetchErrorLogs(true, undefined, errorLogsCursor)} users={adminUsers} />}

        {tab === "overview" && <section aria-label="Visão geral">
          <div className="mb-4"><h2 className="text-base font-medium sm:text-lg">Visão geral & métricas</h2><p className="mt-1 text-sm leading-5 text-zinc-500">Uso atual dos recursos e actividade da plataforma.</p></div>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            <Metric icon={Database} label="Base PostgreSQL" value={stats?.database_size} loading={loadingStats} helper="Tamanho total do banco" />
            <Metric icon={HardDrive} label="Vetores de embedding" value={stats?.embeddings_size} loading={loadingStats} helper="Espaço ocupado por document_chunks" />
            <Metric icon={Users} label="Utilizadores" value={stats?.total_users?.toLocaleString("pt-BR")} loading={loadingStats} helper="Contas cadastradas" />
            <Metric icon={BookOpen} label="Conversas" value={stats?.total_conversations?.toLocaleString("pt-BR")} loading={loadingStats} helper="Históricos armazenados" />
            <Metric icon={FileText} label="Mensagens" value={stats?.total_messages?.toLocaleString("pt-BR")} loading={loadingStats} helper="Interações persistidas" />
            <Metric icon={Atom} label="Chunks indexados" value={stats?.total_chunks?.toLocaleString("pt-BR")} loading={loadingStats} helper="Fragmentos disponíveis para RAG" />
          </div>
          <article className="mt-4 rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6">
            <div className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between"><div className="flex items-start gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl border border-emerald-300/15 bg-emerald-300/[0.06] text-emerald-200"><Server size={18} /></span><div><h3 className="font-medium">Infraestrutura de inferência</h3><p className="mt-1 text-sm text-zinc-500">Conexão verificada com o provedor configurado nesta instalação.</p></div></div><div className="flex items-center gap-2 self-start rounded-full border border-white/10 px-3 py-1.5 text-xs text-zinc-300 sm:self-center"><span className={`size-1.5 rounded-full ${loadingModels ? "animate-pulse bg-amber-300" : connected ? "bg-emerald-400" : "bg-rose-400"}`} />{loadingModels ? "Verificando conexão" : connected ? "Conectado" : "Sem conexão"}</div></div>
            <div className="mt-5 grid gap-3 border-t border-white/[0.06] pt-4 sm:grid-cols-2"><div><p className="text-[11px] uppercase tracking-wider text-zinc-500">Serviço de inferência</p><p className="mt-1 text-sm text-zinc-300">Gerenciado pela configuração desta instalação</p></div><div><p className="text-[11px] uppercase tracking-wider text-zinc-500">Modelos disponíveis</p><p className="mt-1 font-mono text-sm text-zinc-300">{models.length ? models.map((item) => item.name).join(", ") : "Consulte a gestão de modelos"}</p></div></div>
            {providerError && <p className="mt-4 text-xs text-rose-200">{providerError}</p>}
          </article>
        </section>}

        {tab === "users" && <section aria-label="Gestão de utilizadores">
          <div className="mb-5 flex flex-col justify-between gap-3 sm:flex-row sm:items-end"><div><h2 className="text-lg font-medium">Utilizadores</h2><p className="mt-1 text-sm text-zinc-500">Consulte as contas e administre os papéis de acesso.</p></div><span className="inline-flex items-center gap-2 self-start rounded-full border border-white/[0.08] px-3 py-1.5 text-xs text-zinc-400 sm:self-auto"><Users size={14} />{adminUsers.length} contas</span></div>
          {usersFeedback && <p role="status" aria-live="polite" className={`mb-4 rounded-lg border px-3 py-2 text-xs ${usersFeedback.startsWith("Notificação reenviada") ? "border-emerald-300/15 bg-emerald-300/[0.05] text-emerald-200" : "border-amber-300/15 bg-amber-300/[0.05] text-amber-200"}`}>{usersFeedback}</p>}
          <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
            <div className="flex items-center justify-between border-b border-white/[0.07] px-5 py-4"><div><h3 className="font-medium">Contas cadastradas</h3><p className="mt-1 text-xs text-zinc-500">Papéis atualizados no banco de dados imediatamente.</p></div><button onClick={() => void fetchUsers()} aria-label="Atualizar lista de utilizadores" className="grid size-8 place-items-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100"><RefreshCw size={15} /></button></div>
            {loadingUsers ? <div className="space-y-3 p-5"><div className="h-14 animate-pulse rounded-lg bg-white/[0.04]" /><div className="h-14 animate-pulse rounded-lg bg-white/[0.04]" /><div className="h-14 animate-pulse rounded-lg bg-white/[0.04]" /></div> : adminUsers.length === 0 ? <div className="px-5 py-12 text-center"><Users size={24} className="mx-auto text-zinc-600" /><p className="mt-3 text-sm text-zinc-300">Nenhuma conta encontrada</p></div> : <div className="overflow-x-auto"><table className="w-full min-w-[760px] text-left text-sm"><thead className="bg-black/15 text-[11px] uppercase tracking-wider text-zinc-500"><tr><th className="px-5 py-3 font-medium">Nome</th><th className="px-4 py-3 font-medium">E-mail</th><th className="px-4 py-3 font-medium">Papel</th><th className="px-4 py-3 font-medium">Cadastro</th><th className="px-4 py-3" /></tr></thead><tbody className="divide-y divide-white/[0.06]">{adminUsers.map((account) => { const isSelf = account.id === user?.id; return <tr key={account.id} className="transition hover:bg-white/[0.02]"><td className="px-5 py-4"><div className="flex items-center gap-3"><span className="grid size-8 shrink-0 place-items-center rounded-full border border-white/[0.08] bg-white/[0.04] text-xs font-medium text-zinc-300">{account.name.trim().slice(0, 1).toLocaleUpperCase() || "?"}</span><div><p className="font-medium text-zinc-200">{account.name}{isSelf && <span className="ml-2 text-[10px] font-normal text-emerald-200">Você</span>}</p><p className="mt-0.5 font-mono text-[10px] text-zinc-600">{account.id}</p></div></div></td><td className="px-4 py-4 text-xs text-zinc-400">{account.email}</td><td className="px-4 py-4"><label className="sr-only" htmlFor={`role-${account.id}`}>Papel de {account.name}</label><select id={`role-${account.id}`} value={account.role} onChange={(event) => void handleUserRoleChange(account, event.target.value as AdminUser["role"])} disabled={isSelf || updatingUserID === account.id} title={isSelf ? "Não é possível alterar o próprio papel" : "Alterar papel de acesso"} className="h-8 rounded-lg border border-white/10 bg-[#0b0e0d] px-2 text-xs text-zinc-200 outline-none focus:border-emerald-300/40 disabled:cursor-not-allowed disabled:opacity-50"><option value="user">Utilizador</option><option value="admin">Administrador</option></select>{updatingUserID === account.id && <LoaderCircle size={12} className="ml-2 inline animate-spin text-zinc-400" />}</td><td className="px-4 py-4 text-xs text-zinc-500">{formatDate(account.created_at)}</td><td className="px-4 py-4"><div className="flex items-center gap-1">{account.registration_status !== "pending" && <button onClick={() => void resendRegistrationNotification(account)} disabled={resendingUserID === account.id} aria-label={`Reenviar decisão por email para ${account.email}`} title="Reenviar email com a decisão de cadastro" className="grid size-8 place-items-center rounded-lg text-zinc-500 transition hover:bg-emerald-300/[0.08] hover:text-emerald-200 disabled:opacity-40">{resendingUserID === account.id ? <LoaderCircle size={15} className="animate-spin" /> : <Mail size={15} />}</button>}<button onClick={() => setConfirmation({ title: "Remover conta de utilizador?", description: `Remover a conta de ${account.name} (${account.email})? As conversas e mensagens desta conta também serão apagadas. Esta ação não pode ser desfeita.`, confirmLabel: "Remover conta", action: () => handleDeleteUser(account) })} disabled={isSelf || deletingUserID === account.id} aria-label={`Remover conta de ${account.name}`} title={isSelf ? "Não é possível remover a própria conta" : "Remover conta e dados associados"} className="grid size-8 place-items-center rounded-lg text-zinc-500 transition hover:bg-rose-300/[0.08] hover:text-rose-200 disabled:cursor-not-allowed disabled:opacity-30">{deletingUserID === account.id ? <LoaderCircle size={15} className="animate-spin" /> : <Trash2 size={15} />}</button></div></td></tr>; })}</tbody></table></div>}
            <div className="border-t border-white/[0.06] px-5 py-3 text-[11px] leading-5 text-zinc-500">A remoção de uma conta apaga permanentemente as conversas e mensagens associadas. O último administrador não pode ser removido nem rebaixado.</div>
          </article>
        </section>}

        {tab === "access" && <section aria-label="Política de acesso e cadastros">
          <div className="mb-5"><h2 className="text-lg font-medium">Acesso e cadastros</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-zinc-500">Controle quem pode criar conta e quando recebe acesso. Contas pendentes não recebem token e não conseguem usar o chat nem a API.</p></div>
          <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(320px,.82fr)]">
            <div className="space-y-4">
              <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6">
                <div className="flex items-start gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl border border-emerald-300/15 bg-emerald-300/[0.06] text-emerald-200"><ShieldCheck size={18} /></span><div><h3 className="font-medium">Política de aprovação</h3><p className="mt-1 text-xs leading-5 text-zinc-500">A instalação começa em modo de aprovação, até um administrador definir a política desejada.</p></div></div>
                <div className="mt-5 grid gap-3 sm:grid-cols-2">
                  <button type="button" onClick={() => setRegistrationSettings((current) => current ? { ...current, mode: "automatic" } : current)} aria-pressed={registrationSettings?.mode === "automatic"} className={`rounded-xl border p-4 text-left transition ${registrationSettings?.mode === "automatic" ? "border-emerald-300/35 bg-emerald-300/[0.07]" : "border-white/[0.08] bg-black/10 hover:border-white/15"}`}><span className="flex items-center gap-2 text-sm font-medium text-zinc-100"><CheckCircle2 size={16} className={registrationSettings?.mode === "automatic" ? "text-emerald-200" : "text-zinc-500"} />Aprovação automática</span><span className="mt-2 block text-xs leading-5 text-zinc-500">Acesso imediato somente para emails de domínios institucionais ou endereços autorizados.</span></button>
                  <button type="button" onClick={() => setRegistrationSettings((current) => current ? { ...current, mode: "approval" } : current)} aria-pressed={registrationSettings?.mode === "approval"} className={`rounded-xl border p-4 text-left transition ${registrationSettings?.mode === "approval" ? "border-emerald-300/35 bg-emerald-300/[0.07]" : "border-white/[0.08] bg-black/10 hover:border-white/15"}`}><span className="flex items-center gap-2 text-sm font-medium text-zinc-100"><Users size={16} className={registrationSettings?.mode === "approval" ? "text-emerald-200" : "text-zinc-500"} />Análise pelo administrador</span><span className="mt-2 block text-xs leading-5 text-zinc-500">Qualquer pessoa pode pedir cadastro, mas só acessa após aprovação. A rejeição exige justificativa.</span></button>
                </div>
                <div className="mt-5"><label htmlFor="institutional-domains" className="mb-2 block text-xs font-medium text-zinc-300">Domínios institucionais autorizados</label><textarea id="institutional-domains" rows={3} value={allowedDomainsText} onChange={(event) => setAllowedDomainsText(event.target.value)} placeholder={'universidade.edu.br\nescola.org'} disabled={accessBusy} className="w-full resize-y rounded-lg border border-white/10 bg-[#0b0e0d] px-3 py-2.5 font-mono text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-60" /><p className="mt-1.5 text-[11px] leading-5 text-zinc-600">Um domínio por linha (também aceitamos vírgulas). A alteração vale para novos cadastros; não revoga contas já aprovadas.</p></div>
                {accessFeedback && <p role="status" aria-live="polite" className={`mt-4 text-xs leading-5 ${accessFeedback.includes("não foi") || accessFeedback.includes("exige") || accessFeedback.includes("Informe") ? "text-amber-200" : "text-emerald-200"}`}>{accessFeedback}</p>}
                <button type="button" onClick={() => void saveRegistrationSettings()} disabled={accessBusy || !registrationSettings} className="mt-4 inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-50">{accessBusy ? <LoaderCircle size={15} className="animate-spin" /> : <ShieldCheck size={15} />}Salvar política</button>
              </article>

              <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6">
                <div className="flex items-start gap-3"><span className="grid size-9 shrink-0 place-items-center rounded-lg bg-white/[0.04] text-zinc-300"><Mail size={16} /></span><div><h3 className="font-medium">Lista de emails autorizados</h3><p className="mt-1 text-xs leading-5 text-zinc-500">Permita pessoas específicas, independentemente do domínio, no modo automático.</p></div></div>
                <label htmlFor="authorized-emails" className="mb-2 mt-5 block text-xs font-medium text-zinc-300">Emails (um por linha ou separados por vírgula)</label><textarea id="authorized-emails" rows={4} value={authorizedEmailsText} onChange={(event) => setAuthorizedEmailsText(event.target.value)} placeholder="aluno@exemplo.com" disabled={accessBusy} className="w-full resize-y rounded-lg border border-white/10 bg-[#0b0e0d] px-3 py-2.5 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-60" />
                <label htmlFor="allowlist-note" className="mb-2 mt-3 block text-xs font-medium text-zinc-300">Nota administrativa <span className="font-normal text-zinc-600">(opcional)</span></label><input id="allowlist-note" value={allowlistNote} onChange={(event) => setAllowlistNote(event.target.value)} placeholder="Ex.: pesquisador visitante" disabled={accessBusy} className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-60" />
                <button type="button" onClick={() => void addAuthorizedEmails()} disabled={accessBusy || !authorizedEmailsText.trim()} className="mt-3 inline-flex h-9 items-center justify-center gap-2 rounded-lg border border-white/10 px-3 text-xs font-medium text-zinc-200 transition hover:bg-white/[0.05] disabled:opacity-50">{accessBusy ? <LoaderCircle size={14} className="animate-spin" /> : <Users size={14} />}Adicionar à lista</button>
                <div className="mt-5 border-t border-white/[0.06] pt-4"><div className="mb-3 flex items-center justify-between"><h4 className="text-xs font-medium text-zinc-300">Endereços autorizados</h4><span className="text-[11px] text-zinc-500">{authorizedEmails.length}</span></div>{accessBusy && authorizedEmails.length === 0 ? <div className="h-10 animate-pulse rounded-lg bg-white/[0.04]" /> : authorizedEmails.length === 0 ? <p className="rounded-lg border border-dashed border-white/[0.08] px-3 py-5 text-center text-xs text-zinc-600">Nenhum email individual autorizado.</p> : <div className="max-h-64 space-y-1 overflow-y-auto">{authorizedEmails.map((item) => <div key={item.email} className="flex items-center gap-3 rounded-lg px-2 py-2 hover:bg-white/[0.025]"><div className="min-w-0 flex-1"><p className="truncate text-xs text-zinc-200">{item.email}</p>{item.note && <p className="mt-0.5 truncate text-[10px] text-zinc-600">{item.note}</p>}</div><button type="button" onClick={() => void deleteAuthorizedEmail(item.email)} disabled={accessBusy} aria-label={`Remover ${item.email} da lista autorizada`} className="grid size-7 shrink-0 place-items-center rounded-md text-zinc-500 transition hover:bg-rose-300/[0.08] hover:text-rose-200 disabled:opacity-40"><Trash2 size={14} /></button></div>)}</div>}</div>
              </article>
            </div>

            <div className="space-y-4">
              <article className={`rounded-2xl border p-4 ${notificationConfigured ? "border-emerald-300/15 bg-emerald-300/[0.04]" : "border-amber-300/15 bg-amber-300/[0.04]"}`}><div className="flex items-start gap-3"><span className={`mt-0.5 ${notificationConfigured ? "text-emerald-200" : "text-amber-200"}`}>{notificationConfigured ? <CheckCircle2 size={17} /> : <AlertTriangle size={17} />}</span><div><h3 className="text-sm font-medium">{notificationConfigured ? "Notificações por email configuradas" : "Configure o email transacional"}</h3><p className="mt-1 text-xs leading-5 text-zinc-400">{notificationConfigured ? "A decisão de aprovação ou rejeição será enviada ao endereço cadastrado." : "Sem SMTP, a decisão ainda é registrada e o acesso respeita o status, mas o aviso por email não será enviado. Defina SMTP_HOST, SMTP_PORT, SMTP_FROM e, se necessário, SMTP_USERNAME/SMTP_PASSWORD no ambiente da API."}</p></div></div></article>

              <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
                <div className="flex items-center justify-between gap-3 border-b border-white/[0.07] px-5 py-4"><div><h3 className="font-medium">Cadastros aguardando análise</h3><p className="mt-1 text-xs text-zinc-500">{adminUsers.filter((account) => account.registration_status === "pending").length} solicitações pendentes</p></div><button type="button" onClick={() => void fetchUsers()} aria-label="Atualizar solicitações pendentes" className="grid size-8 shrink-0 place-items-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100"><RefreshCw size={15} className={loadingUsers ? "animate-spin" : ""} /></button></div>
                {loadingUsers ? <div className="space-y-3 p-5"><div className="h-24 animate-pulse rounded-lg bg-white/[0.04]" /><div className="h-24 animate-pulse rounded-lg bg-white/[0.04]" /></div> : adminUsers.filter((account) => account.registration_status === "pending").length === 0 ? <div className="px-5 py-10 text-center"><CheckCircle2 size={22} className="mx-auto text-zinc-600" /><p className="mt-3 text-sm text-zinc-300">Nenhum cadastro pendente</p><p className="mt-1 text-xs text-zinc-500">Novas solicitações aparecerão aqui.</p></div> : <div className="divide-y divide-white/[0.06]">{adminUsers.filter((account) => account.registration_status === "pending").map((account) => <div key={account.id} className="p-4 sm:p-5"><div className="flex items-start gap-3"><span className="grid size-9 shrink-0 place-items-center rounded-full border border-amber-300/15 bg-amber-300/[0.06] text-xs font-medium text-amber-100">{account.name.trim().slice(0, 1).toLocaleUpperCase() || "?"}</span><div className="min-w-0 flex-1"><p className="truncate text-sm font-medium text-zinc-200">{account.name}</p><p className="mt-0.5 break-all text-xs text-zinc-500">{account.email}</p><p className="mt-1 text-[10px] text-zinc-600">Solicitado em {formatDate(account.created_at)}</p></div><span className="rounded-full border border-amber-300/15 bg-amber-300/[0.05] px-2 py-1 text-[10px] text-amber-200">Pendente</span></div>
                  <label htmlFor={`rejection-${account.id}`} className="mb-1.5 mt-4 block text-[11px] font-medium text-zinc-400">Justificativa para rejeição</label><textarea id={`rejection-${account.id}`} rows={2} maxLength={1000} value={rejectionReasons[account.id] || ""} onChange={(event) => setRejectionReasons((current) => ({ ...current, [account.id]: event.target.value }))} placeholder="Explique ao solicitante por que o acesso não foi aprovado…" disabled={reviewingUserID === account.id} className="w-full resize-y rounded-lg border border-white/10 bg-[#0b0e0d] px-3 py-2 text-xs text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-rose-300/30 disabled:opacity-60" />
                  <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:justify-end"><button type="button" onClick={() => void reviewRegistration(account, "rejected")} disabled={reviewingUserID === account.id || !(rejectionReasons[account.id] || "").trim()} className="inline-flex h-9 items-center justify-center gap-2 rounded-lg border border-rose-300/15 px-3 text-xs font-medium text-rose-200 transition hover:bg-rose-300/[0.07] disabled:cursor-not-allowed disabled:opacity-40">{reviewingUserID === account.id ? <LoaderCircle size={14} className="animate-spin" /> : <X size={14} />}Rejeitar</button><button type="button" onClick={() => void reviewRegistration(account, "approved")} disabled={reviewingUserID === account.id} className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-emerald-300 px-3 text-xs font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-50">{reviewingUserID === account.id ? <LoaderCircle size={14} className="animate-spin" /> : <CheckCircle2 size={14} />}Aprovar acesso</button></div>
                </div>)}</div>}
              </article>
            </div>
          </div>
        </section>}

        {tab === "identity" && <section aria-label="Identidade pública da instalação">
          <div className="mb-5"><h2 className="text-lg font-medium">Identidade da instalação</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-zinc-500">As informações são opcionais e ficam vazias por padrão. Configure o nome da universidade, escola, campus ou unidade apenas se quiser exibi-lo no app.</p></div>
          {institutionalBusy && !institutionalSettings ? <div className="h-64 animate-pulse rounded-2xl border border-white/[0.08] bg-[#101513]" /> : institutionalSettings && <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(280px,.72fr)]">
            <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6">
              <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-white/[0.07] bg-black/10 p-4"><input type="checkbox" checked={institutionalSettings.show_institution} onChange={(event) => setInstitutionalSettings((current) => current ? { ...current, show_institution: event.target.checked } : current)} className="mt-0.5 size-4 accent-emerald-300" /><span><span className="block text-sm font-medium text-zinc-100">Exibir identificação institucional</span><span className="mt-1 block text-xs leading-5 text-zinc-500">Quando desativado, o nome, a sigla e a unidade não aparecem nas páginas públicas nem no acesso.</span></span></label>
              <div className="mt-5 space-y-4">
                <div><label htmlFor="institution-name" className="mb-1.5 block text-xs font-medium text-zinc-300">Nome da instituição <span className="font-normal text-zinc-600">(opcional)</span></label><input id="institution-name" maxLength={160} value={institutionalSettings.institution_name} onChange={(event) => setInstitutionalSettings((current) => current ? { ...current, institution_name: event.target.value } : current)} placeholder="Universidade, escola ou organização" className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40" /></div>
                <div className="grid gap-4 sm:grid-cols-2"><div><label htmlFor="institution-acronym" className="mb-1.5 block text-xs font-medium text-zinc-300">Sigla <span className="font-normal text-zinc-600">(opcional)</span></label><input id="institution-acronym" maxLength={32} value={institutionalSettings.institution_acronym} onChange={(event) => setInstitutionalSettings((current) => current ? { ...current, institution_acronym: event.target.value } : current)} placeholder="Ex.: ABC" className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40" /></div><div><label htmlFor="institution-unit" className="mb-1.5 block text-xs font-medium text-zinc-300">Unidade, campus ou curso <span className="font-normal text-zinc-600">(opcional)</span></label><input id="institution-unit" maxLength={120} value={institutionalSettings.institution_unit} onChange={(event) => setInstitutionalSettings((current) => current ? { ...current, institution_unit: event.target.value } : current)} placeholder="Ex.: Campus, departamento ou curso" className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40" /></div></div>
              </div>
              {institutionalFeedback && <p role="status" aria-live="polite" className={`mt-4 text-xs ${institutionalFeedback.includes("salvas") ? "text-emerald-200" : "text-amber-200"}`}>{institutionalFeedback}</p>}
              <button type="button" onClick={() => void saveInstitutionalSettings()} disabled={institutionalBusy} className="mt-5 inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-50">{institutionalBusy ? <LoaderCircle size={15} className="animate-spin" /> : <Building2 size={15} />}Salvar informações</button>
            </article>
            <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5"><p className="text-[10px] font-medium uppercase tracking-[.16em] text-zinc-500">Prévia</p><div className="mt-4 rounded-xl border border-white/[0.07] bg-black/15 p-4"><p className="font-medium tracking-[.1em] text-zinc-300">NÓDULO</p>{institutionalSettings.show_institution && institutionalSettings.institution_name ? <div className="mt-4 border-t border-white/[0.07] pt-3"><p className="text-sm text-zinc-300">{institutionalSettings.institution_name}{institutionalSettings.institution_acronym ? ` · ${institutionalSettings.institution_acronym}` : ""}</p>{institutionalSettings.institution_unit && <p className="mt-1 text-xs text-zinc-500">{institutionalSettings.institution_unit}</p>}</div> : <p className="mt-3 text-xs text-zinc-600">Nenhuma identificação institucional será exibida.</p>}</div><p className="mt-3 text-xs leading-5 text-zinc-600">As definições ficam armazenadas no PostgreSQL da instalação e podem ser alteradas a qualquer momento.</p></article>
          </div>}
        </section>}

        {tab === "uploads" && <section aria-label="Limites de anexos do chat">
          <div className="mb-5"><h2 className="text-lg font-medium">Anexos do chat</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-zinc-500">Defina quantos arquivos cada pessoa pode enviar e os tamanhos permitidos. As alterações são salvas no PostgreSQL e aplicadas aos próximos envios.</p></div>
          {chatUploadBusy && !chatUploadSettings ? <div className="h-64 animate-pulse rounded-2xl border border-white/[0.08] bg-[#101513]" /> : chatUploadSettings && <article className="max-w-4xl rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6">
            <div className="grid gap-4 md:grid-cols-3">
              <div><label htmlFor="chat-max-files" className="mb-1.5 block text-xs font-medium text-zinc-300">Arquivos por mensagem</label><input id="chat-max-files" type="number" min="0" step="1" value={chatUploadSettings.max_files} disabled={chatUploadBusy} onChange={(event) => setChatUploadSettings((current) => current ? { ...current, max_files: Math.max(0, Math.floor(Number(event.target.value) || 0)) } : current)} className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none focus:border-emerald-300/40 disabled:opacity-60" /><p className="mt-1.5 text-[11px] text-zinc-500">0 significa sem limite de quantidade. Padrão: 30.</p></div>
              <div><label htmlFor="chat-max-file-mib" className="mb-1.5 block text-xs font-medium text-zinc-300">Tamanho máximo por arquivo (MiB)</label><input id="chat-max-file-mib" type="number" min="0" step="1" value={chatUploadSettings.max_file_size_mib} disabled={chatUploadBusy} onChange={(event) => setChatUploadSettings((current) => current ? { ...current, max_file_size_mib: Math.max(0, Math.floor(Number(event.target.value) || 0)) } : current)} className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none focus:border-emerald-300/40 disabled:opacity-60" /><p className="mt-1.5 text-[11px] text-zinc-500">0 significa sem limite por arquivo. Padrão: 50 MiB.</p></div>
              <div><label htmlFor="chat-max-total-mib" className="mb-1.5 block text-xs font-medium text-zinc-300">Tamanho total por mensagem (MiB)</label><input id="chat-max-total-mib" type="number" min="0" step="1" value={chatUploadSettings.max_total_size_mib} disabled={chatUploadBusy} onChange={(event) => setChatUploadSettings((current) => current ? { ...current, max_total_size_mib: Math.max(0, Math.floor(Number(event.target.value) || 0)) } : current)} className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none focus:border-emerald-300/40 disabled:opacity-60" /><p className="mt-1.5 text-[11px] text-zinc-500">0 significa sem limite total. Padrão: sem limite.</p></div>
            </div>
            <div className="upload-policy-notice mt-5 rounded-xl border p-4"><p className="upload-policy-notice-title text-xs font-medium">A infraestrutura também define o que pode ser recebido</p><p className="upload-policy-notice-copy mt-1 text-xs leading-5">Ao escolher “sem limite”, confira o espaço temporário em disco, memória, proxy reverso, serviço de inferência e a janela de contexto do modelo. Esses componentes podem ter limites próprios.</p></div>
            {chatUploadFeedback && <p role="status" aria-live="polite" className={`mt-4 text-xs ${chatUploadFeedback.startsWith("Limites") ? "text-emerald-200" : "text-amber-200"}`}>{chatUploadFeedback}</p>}
            <button type="button" onClick={() => void saveChatUploadSettings()} disabled={chatUploadBusy} className="mt-5 inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-50">{chatUploadBusy ? <LoaderCircle size={15} className="animate-spin" /> : <Paperclip size={15} />}Salvar limites</button>
          </article>}
        </section>}

        {tab === "models" && <section aria-label="Gestão de modelos">
          <div className="mb-5"><h2 className="text-lg font-medium">Gestão de modelos (LLMs)</h2><p className="mt-1 text-sm text-zinc-500">Modelos disponibilizados pelo provedor de IA configurado nesta instalação.</p></div>
          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(320px,.75fr)]">
            <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
              <div className="flex items-center justify-between border-b border-white/[0.07] px-5 py-4"><div><h3 className="font-medium">Modelos instalados</h3><p className="mt-1 text-xs text-zinc-500">{models.length} {models.length === 1 ? "modelo" : "modelos"} encontrados</p></div><button onClick={() => void fetchModels()} aria-label="Atualizar modelos" className="grid size-8 place-items-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100"><RefreshCw size={15} /></button></div>
            {loadingModels ? <div className="space-y-3 p-5"><div className="h-12 animate-pulse rounded-lg bg-white/[0.04]" /><div className="h-12 animate-pulse rounded-lg bg-white/[0.04]" /></div> : providerError ? <div className="p-6 text-sm text-rose-200">{providerError}</div> : models.length === 0 ? <div className="px-5 py-12 text-center"><Atom size={24} className="mx-auto text-zinc-600" /><p className="mt-3 text-sm text-zinc-300">Nenhum modelo disponível</p><p className="mt-1 text-xs text-zinc-500">Adicione um modelo para habilitá-lo nesta instalação.</p></div> : <div className="divide-y divide-white/[0.06]">{models.map((model) => <div key={model.name} className="flex items-center justify-between gap-3 px-5 py-4"><div className="flex min-w-0 items-center gap-3"><span className="grid size-9 shrink-0 place-items-center rounded-lg bg-white/[0.04] text-emerald-200"><Atom size={16} /></span><div className="min-w-0"><p className="truncate text-sm font-medium text-zinc-200">{model.name}</p><p className="mt-0.5 text-xs text-zinc-500">{formatBytes(model.size)}</p></div></div><button disabled={deletingModel === model.name} onClick={() => setConfirmation({ title: "Remover modelo instalado?", description: `Remover o modelo “${model.name}” do provedor de IA configurado? Esta ação apaga os arquivos locais associados.`, confirmLabel: "Remover modelo", action: () => handleDeleteModel(model.name) })} aria-label={`Excluir ${model.name}`} className="grid size-9 shrink-0 place-items-center rounded-lg text-zinc-500 transition hover:bg-rose-300/[0.08] hover:text-rose-200 disabled:opacity-50">{deletingModel === model.name ? <LoaderCircle size={16} className="animate-spin" /> : <Trash2 size={16} />}</button></div>)}</div>}
            </article>
            <article className="h-fit rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6"><div className="flex items-start gap-3"><span className="grid size-9 place-items-center rounded-lg border border-emerald-300/15 bg-emerald-300/[0.06] text-emerald-200"><CloudDownload size={17} /></span><div><h3 className="font-medium">Adicionar modelo</h3><p className="mt-1 text-xs leading-5 text-zinc-500">Informe o identificador aceito pelo provedor configurado.</p></div></div>
              <form className="mt-5" onSubmit={(event) => { event.preventDefault(); void handlePull(); }}><label htmlFor="model-name" className="mb-2 block text-xs font-medium text-zinc-300">Identificador do modelo</label><input id="model-name" value={modelName} onChange={(event) => setModelName(event.target.value)} placeholder="Nome ou identificador do modelo" disabled={pulling} className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-60" /><button type="submit" disabled={!modelName.trim() || pulling} className="mt-3 inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-not-allowed disabled:opacity-50">{pulling ? <LoaderCircle size={16} className="animate-spin" /> : <ArrowDownToLine size={16} />}{pulling ? "Adicionando…" : "Adicionar modelo"}</button></form>
              {(pullStatus || pulling) && <div className="mt-5 rounded-xl border border-white/[0.07] bg-black/20 p-3.5" role="status" aria-live="polite"><div className="mb-2 flex items-center justify-between gap-3 text-xs"><span className="truncate text-zinc-300">{pullStatus || "Preparando transferência…"}</span><span className="shrink-0 text-zinc-500">{pullPercent === null ? (pulling ? "Em andamento" : "") : `${pullPercent}%`}</span></div><div className="h-1.5 overflow-hidden rounded-full bg-white/[0.08]"><div className={`h-full rounded-full bg-emerald-300 transition-all duration-300 ${pullPercent === null ? "w-1/3 animate-pulse" : ""}`} style={pullPercent === null ? undefined : { width: `${pullPercent}%` }} /></div>{pulling && <button type="button" onClick={() => pullAbort.current?.abort()} className="mt-3 text-xs text-zinc-500 transition hover:text-zinc-200">Cancelar descarregamento</button>}</div>}
              <p className="mt-4 flex items-start gap-2 text-[11px] leading-5 text-zinc-500"><CircleHelp size={14} className="mt-0.5 shrink-0" />O consumo de disco e memória depende do tamanho do modelo selecionado.</p>
            </article>
          </div>
        </section>}

        {tab === "webSearch" && <section aria-label="Configuração da pesquisa web">
          <div className="mb-5"><h2 className="text-lg font-medium">Pesquisa web e privacidade</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-zinc-500">Escolha se o NÓDULO pode consultar a internet quando o acervo institucional não for suficiente ou a pergunta pedir informações recentes.</p></div>
          {webSearchBusy && !webSearchSettings ? <div className="h-64 animate-pulse rounded-2xl border border-white/[0.08] bg-[#101513]" /> : webSearchSettings && <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(300px,.72fr)]">
            <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5 sm:p-6">
              <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-white/[0.07] bg-black/10 p-4"><input type="checkbox" checked={webSearchSettings.enabled} disabled={webSearchBusy} onChange={(event) => setWebSearchSettings((current) => current ? { ...current, enabled: event.target.checked } : current)} className="mt-0.5 size-4 accent-emerald-300" /><span><span className="block text-sm font-medium text-zinc-100">Permitir pesquisa na internet</span><span className="mt-1 block text-xs leading-5 text-zinc-500">Quando desativado, o chat continua respondendo com o acervo local e o conhecimento geral do modelo.</span></span></label>
              <div className="mt-5 grid gap-4 sm:grid-cols-2">
                <div><label htmlFor="web-search-provider" className="mb-1.5 block text-xs font-medium text-zinc-300">Motor de busca</label><select id="web-search-provider" value={webSearchSettings.provider} disabled={webSearchBusy} onChange={(event) => setWebSearchSettings((current) => current ? { ...current, provider: event.target.value as WebSearchSettings["provider"] } : current)} className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none focus:border-emerald-300/40 disabled:opacity-60"><option value="duckduckgo">DuckDuckGo HTML</option><option value="searxng">SearXNG auto-hospedado</option></select></div>
                <div><label htmlFor="web-search-limit" className="mb-1.5 block text-xs font-medium text-zinc-300">Resultados por consulta</label><input id="web-search-limit" type="number" min={1} max={10} value={webSearchSettings.result_limit} disabled={webSearchBusy} onChange={(event) => setWebSearchSettings((current) => current ? { ...current, result_limit: Math.min(10, Math.max(1, Number(event.target.value) || 1)) } : current)} className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none focus:border-emerald-300/40 disabled:opacity-60" /><p className="mt-1.5 text-[11px] text-zinc-500">De 1 a 10 snippets para cada consulta.</p></div>
              </div>
              {webSearchSettings.provider === "searxng" && <div className="mt-4"><label htmlFor="searxng-endpoint" className="mb-1.5 block text-xs font-medium text-zinc-300">URL base da instância SearXNG</label><input id="searxng-endpoint" type="url" value={webSearchSettings.endpoint} disabled={webSearchBusy} onChange={(event) => setWebSearchSettings((current) => current ? { ...current, endpoint: event.target.value } : current)} placeholder="http://searxng:8080" className="h-11 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-60" /><p className="mt-1.5 text-[11px] leading-5 text-zinc-500">Use o endereço acessível pelo container da API, sem `/search`. Em Docker Compose, use o nome do serviço, por exemplo `http://searxng:8080`.</p></div>}
              {webSearchFeedback && <p role="status" aria-live="polite" className={`mt-4 text-xs ${webSearchFeedback.startsWith("Configurações") ? "text-emerald-200" : "text-amber-200"}`}>{webSearchFeedback}</p>}
              <button type="button" onClick={() => void saveWebSearchSettings()} disabled={webSearchBusy} className="mt-5 inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-50">{webSearchBusy ? <LoaderCircle size={15} className="animate-spin" /> : <Globe size={15} />}Salvar pesquisa web</button>
            </article>
            <aside className="space-y-4">
              <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5"><h3 className="font-medium">DuckDuckGo</h3><p className="mt-2 text-xs leading-5 text-zinc-500">Alternativa simples, sem infraestrutura adicional. Quando a pesquisa for acionada, o texto da pergunta atual é enviado ao DuckDuckGo. O histórico e os arquivos anexados não são enviados.</p></article>
              <article className="rounded-2xl border border-emerald-300/15 bg-emerald-300/[0.035] p-5"><h3 className="font-medium">SearXNG na sua infraestrutura</h3><p className="mt-2 text-xs leading-5 text-zinc-500">Hospede o SearXNG na rede Docker da instalação e configure a URL base aqui. A consulta vai primeiro ao seu SearXNG; os motores configurados nele recebem a requisição a partir da infraestrutura do SearXNG, não diretamente do navegador do utilizador. Isso reduz a exposição do IP do utilizador aos motores, mas não garante anonimato nem impede que motores registrem consultas.</p><p className="mt-3 text-xs leading-5 text-zinc-500">Habilite o formato JSON na configuração `search.formats` do SearXNG. Os próximos chats usam a alteração sem reiniciar a API.</p></article>
            </aside>
          </div>}
        </section>}

        {tab === "knowledge" && <section aria-label="Base de conhecimento">
          <div className="mb-5"><h2 className="text-lg font-medium">Base de conhecimento (RAG)</h2><p className="mt-1 text-sm text-zinc-500">Adicione materiais institucionais e acompanhe a indexação em segundo plano.</p></div>

          <article className="knowledge-guidelines-card mb-5 rounded-2xl border border-emerald-300/15 bg-gradient-to-br from-emerald-300/[0.07] via-[#101513] to-[#101513] p-5 sm:p-6">
            <div className="flex items-start gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl border border-emerald-300/20 bg-emerald-300/[0.08] text-emerald-200"><ShieldCheck size={18} /></span><div><p className="text-[10px] font-semibold uppercase tracking-[.18em] text-emerald-200/80">Conhecimento institucional</p><h3 className="mt-1 text-base font-medium text-zinc-100 sm:text-lg">Diretrizes de Ingestão e Soberania do Conhecimento</h3></div></div>
            <div className="mt-5 grid gap-4 text-sm leading-6 text-zinc-400 md:grid-cols-3">
              <div><p className="font-medium text-zinc-200">Como o RAG funciona</p><p className="mt-1">Os documentos são fragmentados e convertidos em vetores no PostgreSQL (pgvector). O modelo usa esses trechos como contexto factual sem alterar os pesos do modelo original.</p></div>
              <div><p className="font-medium text-zinc-200">Formatos aceitos</p><p className="mt-1">Livros e artigos em PDF (.pdf), notas e código (.md, .txt), pacotes completos em ZIP (.zip) e links diretos da Web.</p></div>
              <div className="knowledge-performance-tip rounded-xl border border-amber-300/10 bg-amber-300/[0.035] p-3"><p className="flex items-center gap-2 font-medium text-amber-100"><AlertTriangle size={15} />Dica de performance</p><p className="mt-1 text-zinc-400">PDFs digitalizados em imagem requerem OCR prévio; prefira PDFs com camada de texto selecionável.</p></div>
            </div>
          </article>

          <div className="grid items-start gap-4 xl:grid-cols-[minmax(350px,.86fr)_minmax(0,1.14fr)]">
            <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
              <div className="border-b border-white/[0.07] px-5 pt-5"><div><h3 className="font-medium">Nova ingestão</h3><p className="mt-1 text-xs text-zinc-500">Arquivos e fontes web são processados em background.</p></div>
                <div className="mt-4 flex gap-1 overflow-x-auto" role="tablist" aria-label="Tipo de fonte"><button role="tab" aria-selected={knowledgeMode === "files"} onClick={() => { setKnowledgeMode("files"); setIngestionFeedback(""); }} className={`inline-flex min-h-10 shrink-0 items-center gap-2 rounded-t-lg border-b-2 px-3 text-sm transition ${knowledgeMode === "files" ? "border-emerald-300 text-emerald-100" : "border-transparent text-zinc-500 hover:text-zinc-200"}`}><FolderArchive size={15} />Arquivos e pacotes</button><button role="tab" aria-selected={knowledgeMode === "web"} onClick={() => { setKnowledgeMode("web"); setIngestionFeedback(""); }} className={`inline-flex min-h-10 shrink-0 items-center gap-2 rounded-t-lg border-b-2 px-3 text-sm transition ${knowledgeMode === "web" ? "border-emerald-300 text-emerald-100" : "border-transparent text-zinc-500 hover:text-zinc-200"}`}><Globe size={15} />Fonte web</button></div>
              </div>

              {knowledgeMode === "files" ? <div className="space-y-4 p-5">
                <div><label htmlFor="batch-title" className="mb-1.5 block text-xs font-medium text-zinc-300">Título <span className="font-normal text-zinc-600">(opcional para arquivo único)</span></label><input id="batch-title" value={fileTitle} onChange={(event) => setFileTitle(event.target.value)} placeholder="Usar o nome original do arquivo" disabled={selectedFiles.length > 1 || ingestionBusy} className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-50" /></div>
                <div><label htmlFor="batch-reference" className="mb-1.5 block text-xs font-medium text-zinc-300">Disciplina / Referência Geral</label><input id="batch-reference" value={fileReference} onChange={(event) => setFileReference(event.target.value)} placeholder="ENG0001 · Materiais da disciplina" disabled={ingestionBusy} className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-50" /></div>
                <input ref={fileInput} type="file" multiple accept=".pdf,.txt,.md,.zip,application/pdf,text/plain,text/markdown,application/zip" onChange={handleFileChange} className="sr-only" aria-label="Selecionar arquivos PDF, texto, Markdown ou ZIP" />
                <button type="button" onClick={() => fileInput.current?.click()} onDragOver={(event) => { event.preventDefault(); setDraggingFiles(true); }} onDragLeave={() => setDraggingFiles(false)} onDrop={handleDrop} disabled={ingestionBusy} className={`flex min-h-36 w-full flex-col items-center justify-center rounded-xl border border-dashed px-4 py-5 text-center transition disabled:cursor-wait ${draggingFiles ? "border-emerald-300/60 bg-emerald-300/[0.08]" : "border-white/15 bg-black/10 hover:border-white/25 hover:bg-white/[0.02]"}`}><span className="grid size-10 place-items-center rounded-xl bg-white/[0.05] text-emerald-200">{selectedFiles.some((selected) => selected.name.toLowerCase().endsWith(".zip")) ? <FolderArchive size={18} /> : <Upload size={18} />}</span><span className="mt-3 text-sm font-medium text-zinc-200">Arraste arquivos ou clique para selecionar</span><span className="mt-1 text-xs text-zinc-500">PDF, TXT, MD ou ZIP · até 20 arquivos · máximo 200 MB por lote</span></button>
                {selectedFiles.length > 0 && <div className="space-y-2 rounded-xl border border-white/[0.07] bg-black/15 p-3"><div className="flex items-center justify-between text-xs"><span className="font-medium text-zinc-300">{selectedFiles.length} {selectedFiles.length === 1 ? "arquivo selecionado" : "arquivos selecionados"}</span><span className="text-zinc-500">{formatBytes(totalSelectedBytes)}</span></div><div className="max-h-32 space-y-1 overflow-y-auto">{selectedFiles.map((selected, index) => <div key={`${selected.name}:${selected.lastModified}:${index}`} className="flex items-center gap-2 text-xs text-zinc-400"><FileText size={13} className="shrink-0 text-zinc-500" /><span className="min-w-0 flex-1 truncate" title={selected.name}>{selected.name}</span><span className="shrink-0 text-zinc-600">{formatBytes(selected.size)}</span><button type="button" onClick={() => setSelectedFiles((current) => current.filter((_, currentIndex) => currentIndex !== index))} aria-label={`Remover ${selected.name}`} className="grid size-6 shrink-0 place-items-center rounded text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200"><X size={13} /></button></div>)}</div></div>}
                {ingestionFeedback && <p role={ingestionFeedback.includes("job enviado") || ingestionFeedback.includes("jobs enviados") ? "status" : "alert"} aria-live="polite" className={`rounded-lg px-3 py-2 text-xs leading-5 ${ingestionFeedback.includes("job enviado") || ingestionFeedback.includes("jobs enviados") ? "border border-emerald-300/15 bg-emerald-300/[0.05] text-emerald-200" : "border border-rose-300/15 bg-rose-300/[0.05] text-rose-200"}`}>{ingestionFeedback}</p>}
                <button type="button" disabled={ingestionBusy || selectedFiles.length === 0 || !fileReference.trim()} onClick={() => void handleUploadBatch()} className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-not-allowed disabled:opacity-50">{ingestionBusy ? <><LoaderCircle size={16} className="animate-spin" />Enviando lote…</> : <><Upload size={16} />Iniciar Processamento em Lote</>}</button>
              </div> : <div className="space-y-4 p-5">
                <div><label htmlFor="web-url" className="mb-1.5 block text-xs font-medium text-zinc-300">URL da página</label><div className="relative"><Globe size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500" /><input id="web-url" type="url" value={webURL} onChange={(event) => setWebURL(event.target.value)} placeholder="https://pt.wikipedia.org/wiki/..." disabled={ingestionBusy} className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] pl-9 pr-3 text-sm outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-50" /></div></div>
                <div><label htmlFor="web-title" className="mb-1.5 block text-xs font-medium text-zinc-300">Título</label><input id="web-title" value={webTitle} onChange={(event) => setWebTitle(event.target.value)} placeholder="Título do artigo ou recurso" disabled={ingestionBusy} className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-50" /></div>
                <div><label htmlFor="web-reference" className="mb-1.5 block text-xs font-medium text-zinc-300">Referência institucional</label><input id="web-reference" value={webReference} onChange={(event) => setWebReference(event.target.value)} placeholder="ENG0001 · Bibliografia complementar" disabled={ingestionBusy} className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-sm outline-none placeholder:text-zinc-600 focus:border-emerald-300/40 disabled:opacity-50" /></div>
                <div className="rounded-xl border border-white/[0.06] bg-black/15 p-3 text-xs leading-5 text-zinc-500"><ShieldCheck size={14} className="mr-1.5 inline text-emerald-200" />Páginas de redes privadas ou endereços locais são bloqueadas pelo servidor.</div>
                {ingestionFeedback && <p role={ingestionFeedback.includes("adicionada à fila") ? "status" : "alert"} aria-live="polite" className={`rounded-lg px-3 py-2 text-xs leading-5 ${ingestionFeedback.includes("adicionada à fila") ? "border border-emerald-300/15 bg-emerald-300/[0.05] text-emerald-200" : "border border-rose-300/15 bg-rose-300/[0.05] text-rose-200"}`}>{ingestionFeedback}</p>}
                <button type="button" disabled={ingestionBusy || !webURL.trim() || !webTitle.trim() || !webReference.trim()} onClick={() => void handleScrapeURL()} className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-emerald-300 px-4 text-sm font-semibold text-[#102018] transition hover:bg-emerald-200 disabled:cursor-not-allowed disabled:opacity-50">{ingestionBusy ? <><LoaderCircle size={16} className="animate-spin" />Enfileirando captura…</> : <><Globe size={16} />Capturar e Indexar Página</>}</button>
              </div>}
            </article>

            <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
              <div className="flex items-center justify-between gap-3 border-b border-white/[0.07] px-5 py-4"><div><h3 className="font-medium">Monitor de processamento</h3><p className="mt-1 text-xs text-zinc-500">Atualização em tempo real pelo stream SSE.</p></div><button onClick={() => void fetchJobs(true)} aria-label="Atualizar jobs" className="grid size-8 shrink-0 place-items-center rounded-lg text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100"><RefreshCw size={15} className={jobsLoading ? "animate-spin" : ""} /></button></div>
              <div className="flex items-center justify-between border-b border-white/[0.05] px-5 py-2.5 text-xs"><span className="text-zinc-500">{jobs.length} jobs recentes</span><span className={`inline-flex items-center gap-1.5 ${activeJobsCount ? "text-sky-200" : "text-zinc-500"}`}><span className={`size-1.5 rounded-full ${activeJobsCount ? "animate-pulse bg-sky-300" : "bg-zinc-600"}`} />{activeJobsCount} ativos</span></div>
              {jobsError && <div role="alert" className="mx-5 mt-4 rounded-lg border border-rose-300/15 bg-rose-300/[0.05] px-3 py-2 text-xs text-rose-200">{jobsError}</div>}
              {jobsNotice && <div role="status" aria-live="polite" className="mx-5 mt-4 rounded-lg border border-emerald-300/15 bg-emerald-300/[0.05] px-3 py-2 text-xs text-emerald-200">{jobsNotice}</div>}
              {jobsLoading ? <div className="space-y-3 p-5"><div className="h-24 animate-pulse rounded-xl bg-white/[0.04]" /><div className="h-24 animate-pulse rounded-xl bg-white/[0.04]" /></div> : jobs.length === 0 ? <div className="px-5 py-12 text-center"><CheckCircle2 size={24} className="mx-auto text-zinc-600" /><p className="mt-3 text-sm text-zinc-300">Nenhum processamento recente</p><p className="mt-1 text-xs text-zinc-500">Jobs aparecerão aqui assim que uma ingestão começar.</p></div> : <div className="max-h-[520px] space-y-3 overflow-y-auto p-4">{jobs.slice(0, 8).map((job) => <JobCard key={job.id} job={job} onRetry={(target, file) => { void retryJob(target, file); }} retrying={retryingJobID === job.id} />)}</div>}
            </article>
          </div>

          <article className="mt-5 overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
            <div className="flex flex-col gap-4 border-b border-white/[0.07] px-5 py-4 sm:flex-row sm:items-center sm:justify-between"><div><h3 className="font-medium">Acervo indexado</h3><p className="mt-1 text-xs text-zinc-500">Documentos e fragmentos disponíveis para recuperação semântica.</p></div><div className="flex flex-wrap items-center gap-2"><span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-300/15 bg-emerald-300/[0.045] px-2.5 py-1.5 text-xs text-emerald-100"><FileText size={13} />{documents.length} documentos</span><span className="inline-flex items-center gap-1.5 rounded-full border border-white/[0.08] px-2.5 py-1.5 text-xs text-zinc-300"><FolderArchive size={13} />{stats?.total_chunks?.toLocaleString("pt-BR") ?? "—"} chunks</span></div></div>
            <div className="flex flex-col gap-3 border-b border-white/[0.05] px-5 py-3 sm:flex-row sm:items-center sm:justify-between"><label className="relative block w-full sm:max-w-sm"><Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500" /><input type="search" value={documentSearch} onChange={(event) => setDocumentSearch(event.target.value)} placeholder="Buscar por título ou referência" className="h-9 w-full rounded-lg border border-white/10 bg-[#0b0e0d] pl-9 pr-3 text-xs text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/35" /></label><button onClick={() => { void fetchDocuments(); void fetchStats(); }} aria-label="Atualizar acervo" className="inline-flex h-9 items-center justify-center gap-2 self-start rounded-lg border border-white/10 px-3 text-xs text-zinc-300 transition hover:bg-white/[0.05] sm:self-auto"><RefreshCw size={13} />Atualizar acervo</button></div>
            {loadingDocuments ? <div className="space-y-3 p-5"><div className="h-12 animate-pulse rounded-lg bg-white/[0.04]" /><div className="h-12 animate-pulse rounded-lg bg-white/[0.04]" /><div className="h-12 animate-pulse rounded-lg bg-white/[0.04]" /></div> : filteredDocuments.length === 0 ? <div className="px-5 py-12 text-center"><BookOpen size={24} className="mx-auto text-zinc-600" /><p className="mt-3 text-sm text-zinc-300">{documents.length === 0 ? "O acervo ainda está vazio" : "Nenhum resultado para esta busca"}</p><p className="mt-1 text-xs text-zinc-500">{documents.length === 0 ? "Os materiais indexados aparecerão aqui." : "Tente outro título ou referência institucional."}</p></div> : <div className="overflow-x-auto"><table className="w-full min-w-[700px] text-left text-sm"><thead className="bg-black/15 text-[11px] uppercase tracking-wider text-zinc-500"><tr><th className="px-5 py-3 font-medium">Material</th><th className="px-4 py-3 font-medium">Referência</th><th className="px-4 py-3 font-medium">Chunks</th><th className="px-4 py-3 font-medium">Última ingestão</th><th className="px-4 py-3" /></tr></thead><tbody className="divide-y divide-white/[0.06]">{visibleDocuments.map((document) => <tr key={`${document.source_reference}:${document.title}`} className="transition hover:bg-white/[0.02]"><td className="max-w-64 px-5 py-4"><p className="truncate font-medium text-zinc-200" title={document.title}>{document.title}</p></td><td className="max-w-64 px-4 py-4"><p className="truncate text-xs text-zinc-400" title={document.source_reference}>{document.source_reference}</p></td><td className="px-4 py-4 text-zinc-300">{document.chunks_count.toLocaleString("pt-BR")}</td><td className="px-4 py-4 text-xs text-zinc-500">{formatDate(document.last_ingested_at)}</td><td className="px-4 py-4"><button onClick={() => setConfirmation({ title: "Apagar os vetores deste material?", description: `Apagar todos os vetores de “${document.title}” (${document.source_reference})? Esta ação não pode ser desfeita.`, confirmLabel: "Apagar vetores", action: () => handleDeleteDocument(document) })} disabled={deletingSource === document.source_reference} aria-label={`Excluir ${document.title}`} className="grid size-8 place-items-center rounded-lg text-zinc-500 transition hover:bg-rose-300/[0.08] hover:text-rose-200 disabled:opacity-50">{deletingSource === document.source_reference ? <LoaderCircle size={15} className="animate-spin" /> : <Trash2 size={15} />}</button></td></tr>)}</tbody></table></div>}
            <div className="flex flex-col gap-3 border-t border-white/[0.06] px-5 py-3 sm:flex-row sm:items-center sm:justify-between"><p className="text-xs text-zinc-500">{filteredDocuments.length ? `Exibindo ${(documentPage - 1) * documentPageSize + 1}–${Math.min(documentPage * documentPageSize, filteredDocuments.length)} de ${filteredDocuments.length} materiais` : ""}</p><div className="flex items-center gap-2"><button disabled={documentPage <= 1} onClick={() => setDocumentPage((page) => Math.max(1, page - 1))} aria-label="Página anterior" className="grid size-8 place-items-center rounded-lg border border-white/10 text-zinc-300 hover:bg-white/[0.05] disabled:cursor-not-allowed disabled:opacity-30"><ChevronLeft size={15} /></button><span className="min-w-16 text-center text-xs text-zinc-500">{documentPage} / {documentPageCount}</span><button disabled={documentPage >= documentPageCount} onClick={() => setDocumentPage((page) => Math.min(documentPageCount, page + 1))} aria-label="Próxima página" className="grid size-8 place-items-center rounded-lg border border-white/10 text-zinc-300 hover:bg-white/[0.05] disabled:cursor-not-allowed disabled:opacity-30"><ChevronRight size={15} /></button></div></div>
          </article>
        </section>}

        <footer className="mt-10 flex flex-col gap-2 border-t border-white/[0.06] pt-5 text-xs text-zinc-600 sm:flex-row sm:items-center sm:justify-between"><span className="inline-flex items-center gap-2"><ShieldCheck size={13} />Área restrita a administradores · {user?.email}</span><span>NÓDULO · Governança local de IA</span></footer>
      </div>
      <ConfirmModal
        open={confirmation !== null}
        title={confirmation?.title ?? "Confirmar ação"}
        description={confirmation?.description ?? ""}
        confirmLabel={confirmation?.confirmLabel ?? "Confirmar"}
        busy={confirmationBusy}
        onConfirm={runConfirmation}
        onCancel={() => { if (!confirmationBusy) setConfirmation(null); }}
      />
    </main>
  );
}

function formatUptime(seconds: number) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor(seconds % 86400 / 3600);
  const minutes = Math.floor(seconds % 3600 / 60);
  return days ? `${days}d ${hours}h ${minutes}min` : hours ? `${hours}h ${minutes}min` : `${minutes}min`;
}

function ObservabilityPanel({ data, error, loading, logs, logsLoading, logsError, logQuery, onLogQueryChange, logLevel, onLogLevelChange, onRefreshLogs, hasMoreLogs, onLoadMoreLogs, users }: {
  data: ObservabilityData | null; error: string; loading: boolean; logs: ErrorLog[]; logsLoading: boolean; logsError: string;
  logQuery: string; onLogQueryChange: (value: string) => void; logLevel: string; onLogLevelChange: (value: string) => void;
  onRefreshLogs: () => void; hasMoreLogs: boolean; onLoadMoreLogs: () => void; users: AdminUser[];
}) {
  const buckets = data?.metrics.requests_per_minute ?? [];
  const maxRequests = Math.max(1, ...buckets.map((bucket) => bucket.requests));
  const statusEntries = Object.entries(data?.metrics.status_counts ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return <section aria-label="Monitoramento e observabilidade" className="space-y-5">
    <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-end"><div><h2 className="text-lg font-medium">Monitoramento e observabilidade</h2><p className="mt-1 text-sm text-zinc-500">Saúde dos serviços, atividade HTTP, recursos do processo e trilha de auditoria. Atualiza a cada 10 segundos.</p></div><p className="text-xs text-zinc-500">{data ? `Processo iniciado ${formatDate(data.metrics.started_at)}` : ""}</p></div>
    {error && <div role="alert" className="rounded-xl border border-rose-300/15 bg-rose-300/[0.06] px-4 py-3 text-sm text-rose-200">{error}</div>}
    {!data && loading ? <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{Array.from({length:8},(_,index)=><div key={index} className="h-28 animate-pulse rounded-2xl border border-white/[0.08] bg-[#101513]" />)}</div> : data && <>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Metric icon={Activity} label="Requisições HTTP" value={data.metrics.total_requests.toLocaleString("pt-BR")} loading={false} helper={`${data.metrics.in_flight} em andamento`} />
        <Metric icon={Gauge} label="Tempo ativo" value={formatUptime(data.metrics.uptime_seconds)} loading={false} helper="Desde a última inicialização da API" />
        <Metric icon={Server} label="Memória alocada" value={formatBytes(data.process.memory_alloc_bytes)} loading={false} helper={`Reservada pelo runtime: ${formatBytes(data.process.memory_sys_bytes)}`} />
        <Metric icon={RefreshCw} label="Goroutines" value={data.process.goroutines.toLocaleString("pt-BR")} loading={false} helper={`${data.process.gc_count.toLocaleString("pt-BR")} ciclos de coleta de memória`} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.6fr_1fr]">
        <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5">
          <div className="flex items-start justify-between gap-3"><div><h3 className="font-medium">Tráfego da API</h3><p className="mt-1 text-xs text-zinc-500">Requisições por minuto · últimos 60 minutos</p></div><span className="inline-flex items-center gap-1.5 rounded-full border border-white/[0.08] px-2.5 py-1 text-[11px] text-zinc-400"><Activity size={12} />{data.metrics.total_requests} total</span></div>
          <div className="mt-6 flex h-36 items-end gap-1" role="img" aria-label="Gráfico de requisições por minuto nos últimos 60 minutos">{buckets.map((bucket,index)=><div key={index} title={`${bucket.requests} solicitações · ${bucket.errors} erros`} className="group relative flex h-full min-w-0 flex-1 items-end"><div className="w-full rounded-t-sm bg-emerald-300/70 transition-colors group-hover:bg-emerald-200" style={{height:`${Math.max(bucket.requests ? 5 : 1, bucket.requests/maxRequests*100)}%`}}/><div className="absolute bottom-0 w-full rounded-t-sm bg-rose-400/80" style={{height:`${bucket.errors ? Math.max(3,bucket.errors/maxRequests*100) : 0}%`}} /></div>)}</div>
          <div className="mt-2 flex justify-between text-[10px] text-zinc-600"><span>60 min atrás</span><span>Agora</span></div>
          <div className="mt-4 flex flex-wrap gap-2">{statusEntries.map(([status,count])=><span key={status} className="rounded-full border border-white/[0.08] px-2.5 py-1 text-[11px] text-zinc-400">{status}: {count.toLocaleString("pt-BR")}</span>)}</div>
        </article>
        <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5">
          <h3 className="font-medium">Saúde das dependências</h3><p className="mt-1 text-xs text-zinc-500">Verificações ativas com timeout de 3 segundos.</p>
          <div className="mt-4 space-y-3">{Object.entries(data.dependencies).map(([key,dependency])=><div key={key} className="flex items-center justify-between gap-3 rounded-xl border border-white/[0.06] bg-black/10 p-3"><div className="flex items-center gap-3"><span className={`grid size-8 place-items-center rounded-lg ${dependency.status === "ok" ? "bg-emerald-300/[0.08] text-emerald-200" : "bg-rose-300/[0.08] text-rose-200"}`}>{key === "database" ? <Database size={15} /> : <Server size={15} />}</span><div><p className="text-sm font-medium text-zinc-200">{key === "database" ? "Banco de dados" : "Serviço de inferência"}</p><p className={`mt-0.5 text-[11px] ${dependency.status === "ok" ? "text-emerald-200" : "text-rose-200"}`}>{dependency.status === "ok" ? "Disponível" : "Indisponível"}{key === "inference" && dependency.status === "ok" ? ` · ${dependency.available_models ?? 0} modelos` : ""}</p></div></div><span className="shrink-0 font-mono text-xs text-zinc-400">{dependency.latency_ms} ms</span></div>)}</div>
        </article>
      </div>

      <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
        <div className="border-b border-white/[0.07] px-5 py-4"><h3 className="font-medium">Rotas mais utilizadas</h3><p className="mt-1 text-xs text-zinc-500">Métricas agrupadas pelo padrão da rota para evitar expor IDs e limitar cardinalidade.</p></div>
        {data.metrics.routes.length ? <div className="overflow-x-auto"><table className="w-full min-w-[620px] text-left text-sm"><thead className="bg-black/15 text-[10px] uppercase tracking-wider text-zinc-500"><tr><th className="px-5 py-3 font-medium">Rota</th><th className="px-4 py-3 font-medium">Requisições</th><th className="px-4 py-3 font-medium">Erros</th><th className="px-4 py-3 font-medium">Duração média</th></tr></thead><tbody className="divide-y divide-white/[0.06]">{data.metrics.routes.map((route)=><tr key={route.route}><td className="px-5 py-3 font-mono text-xs text-zinc-300">{route.route}</td><td className="px-4 py-3 text-zinc-300">{route.count.toLocaleString("pt-BR")}</td><td className={`px-4 py-3 ${route.errors ? "text-rose-200" : "text-zinc-500"}`}>{route.errors.toLocaleString("pt-BR")}</td><td className="px-4 py-3 font-mono text-xs text-zinc-400">{route.average_duration_ms.toFixed(1)} ms</td></tr>)}</tbody></table></div> : <p className="px-5 py-8 text-sm text-zinc-500">Ainda não há métricas de requisições.</p>}
      </article>

      <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
        <div className="flex flex-col justify-between gap-3 border-b border-white/[0.07] px-5 py-4 sm:flex-row sm:items-center"><div><h3 className="font-medium">Erros e logs de diagnóstico</h3><p className="mt-1 text-xs leading-5 text-zinc-500">Requisições recusadas, falhas da aplicação e avisos. Registos persistidos no PostgreSQL.</p></div><span className="inline-flex items-center gap-1.5 self-start rounded-full border border-rose-300/15 bg-rose-300/[0.05] px-2.5 py-1 text-[11px] text-rose-200"><AlertTriangle size={12} />{logs.length} exibidos</span></div>
        {(data.metrics.log_persistence_failures > 0 || data.metrics.dropped_errors > 0) && <p role="status" className="mx-4 mt-4 rounded-lg border border-amber-300/20 bg-amber-300/[0.06] px-3 py-2 text-xs leading-5 text-amber-200">Atenção: {data.metrics.log_persistence_failures} registos falharam ao gravar no PostgreSQL e {data.metrics.dropped_errors} excederam a fila temporária desde a inicialização. Consulte também os logs do processo/container.</p>}
        <form onSubmit={(event) => { event.preventDefault(); onRefreshLogs(); }} className="flex flex-col gap-2 border-b border-white/[0.06] p-4 sm:flex-row">
          <label className="relative min-w-0 flex-1"><span className="sr-only">Buscar erros e logs</span><Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500" /><input value={logQuery} onChange={(event) => onLogQueryChange(event.target.value)} placeholder="Buscar por rota, request ID, utilizador ou erro" className="h-10 w-full rounded-lg border border-white/10 bg-[#0b0e0d] pl-9 pr-3 text-xs text-zinc-100 outline-none placeholder:text-zinc-600 focus:border-emerald-300/40" /></label>
          <select value={logLevel} onChange={(event) => onLogLevelChange(event.target.value)} aria-label="Filtrar por gravidade" className="h-10 rounded-lg border border-white/10 bg-[#0b0e0d] px-3 text-xs text-zinc-200 outline-none focus:border-emerald-300/40"><option value="">Todos os níveis</option><option value="ERROR">Erro</option><option value="WARN">Aviso</option></select>
          <button type="submit" disabled={logsLoading} className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-white/10 px-3 text-xs text-zinc-300 transition hover:bg-white/[0.05] disabled:opacity-50"><Search size={13} />Buscar</button>
        </form>
        {logsError && <p role="alert" className="mx-4 mt-4 rounded-lg border border-rose-300/15 bg-rose-300/[0.05] px-3 py-2 text-xs text-rose-200">{logsError}</p>}
        {logsLoading && logs.length === 0 ? <div className="space-y-3 p-4"><div className="h-24 animate-pulse rounded-xl bg-white/[0.04]" /><div className="h-24 animate-pulse rounded-xl bg-white/[0.04]" /></div> : logs.length === 0 ? <div className="px-5 py-10 text-center"><CheckCircle2 size={22} className="mx-auto text-emerald-200/70" /><p className="mt-2 text-sm text-zinc-300">Nenhum erro encontrado</p><p className="mt-1 text-xs text-zinc-500">Tente outra busca ou selecione todos os níveis.</p></div> : <div className="max-h-[720px] space-y-2 overflow-y-auto p-3 sm:p-4">{logs.map((entry) => {
          const actor = users.find((account) => account.id === entry.actor_user_id);
          const failed = entry.level === "ERROR";
          const sourceLabel = entry.source === "http" ? "Requisição HTTP" : entry.source === "legacy_log" ? "Log do servidor" : "Aplicação";
          return <article key={entry.id} className="rounded-xl border border-white/[0.07] bg-black/10 p-3 sm:p-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between"><div className="flex min-w-0 items-start gap-2.5"><span className={`mt-0.5 shrink-0 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-wide ${failed ? "border-rose-300/20 bg-rose-300/[0.07] text-rose-200" : "border-amber-300/20 bg-amber-300/[0.07] text-amber-200"}`}>{failed ? "Erro" : "Aviso"}</span><div className="min-w-0"><p className="break-words text-sm font-medium text-zinc-200">{entry.message}</p><p className="mt-1 text-[11px] text-zinc-500">{sourceLabel}{entry.status_code ? ` · HTTP ${entry.status_code}` : ""}</p></div></div><time className="shrink-0 pl-10 text-[11px] text-zinc-500 sm:pl-0">{formatDate(entry.occurred_at)}</time></div>
            <div className="mt-3 grid gap-x-4 gap-y-2 rounded-lg border border-white/[0.05] bg-white/[0.015] p-2.5 text-[11px] sm:grid-cols-2 xl:grid-cols-4"><div className="min-w-0"><span className="text-zinc-600">Local </span><span className="break-all font-mono text-zinc-300">{entry.method ? `${entry.method} ` : ""}{entry.route || "(fora de uma requisição HTTP)"}</span></div><div className="min-w-0"><span className="text-zinc-600">Utilizador </span><span className="break-all text-zinc-300">{actor ? `${actor.name} · ${actor.email}` : entry.actor_user_id || "Não autenticado / serviço interno"}</span></div><div className="min-w-0"><span className="text-zinc-600">Request ID </span><span className="break-all font-mono text-zinc-300">{entry.request_id || "—"}</span></div><div className="min-w-0"><span className="text-zinc-600">Registo </span><span className="text-zinc-300">{entry.id}</span></div></div>
            {entry.details && <details className="error-log-details mt-2"><summary>Ver diagnóstico técnico</summary><pre>{entry.details}</pre></details>}
          </article>;
        })}</div>}
        {hasMoreLogs && <div className="border-t border-white/[0.06] p-3 text-center"><button type="button" disabled={logsLoading} onClick={onLoadMoreLogs} className="inline-flex min-h-9 items-center gap-2 rounded-lg border border-white/10 px-3 text-xs text-zinc-300 hover:bg-white/[0.05] disabled:opacity-50">{logsLoading ? <LoaderCircle size={13} className="animate-spin" /> : <ChevronDown size={13} />}Carregar registos anteriores</button></div>}
        <p className="border-t border-white/[0.06] px-5 py-3 text-[11px] leading-5 text-zinc-600">Os registos não guardam corpo de pedidos, prompts, tokens ou conteúdo de anexos. Cada entrada pode ser correlacionada com o log do processo pelo request ID.</p>
      </article>

      <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
        <div className="border-b border-white/[0.07] px-5 py-4"><h3 className="font-medium">Auditoria recente</h3><p className="mt-1 text-xs text-zinc-500">Ações administrativas e de segurança, sem conteúdo de mensagens ou credenciais.</p></div>
        {data.metrics.recent_audit.length ? <div className="divide-y divide-white/[0.06]">{data.metrics.recent_audit.slice(0,20).map((event,index)=><div key={`${event.request_id ?? event.time}-${index}`} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-5 py-3"><div className="min-w-0"><p className="truncate text-sm text-zinc-200">{event.event}</p><p className="mt-0.5 font-mono text-[10px] text-zinc-600">{event.request_id || "sem request ID"}{event.actor_user_id ? ` · utilizador ${event.actor_user_id}` : ""}</p></div><time className="shrink-0 text-[11px] text-zinc-500">{formatDate(event.time)}</time></div>)}</div> : <p className="px-5 py-8 text-sm text-zinc-500">Nenhum evento de auditoria foi registado desde a inicialização.</p>}
        <p className="border-t border-white/[0.06] px-5 py-3 text-[11px] leading-5 text-zinc-600">As métricas de processo e os eventos de auditoria recentes são temporários. O histórico de erros e avisos desta instalação fica persistido no PostgreSQL.</p>
      </article>
    </>}
  </section>;
}

function Metric({ icon: Icon, label, value, loading, helper }: { icon: typeof Gauge; label: string; value?: string; loading: boolean; helper: string }) {
  return <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-4 sm:p-5"><div className="flex items-start justify-between gap-3"><div><p className="text-xs font-medium text-zinc-400">{label}</p>{loading ? <div className="mt-3 h-8 w-24 animate-pulse rounded bg-white/[0.06]" /> : <p className="mt-2 text-2xl font-medium tracking-tight text-zinc-100">{value ?? "—"}</p>}</div><span className="grid size-9 place-items-center rounded-xl border border-white/[0.07] bg-white/[0.025] text-emerald-200/80"><Icon size={17} /></span></div><p className="mt-3 text-xs text-zinc-600">{helper}</p></article>;
}

export default function AdminPage() {
  return <ProtectedPage adminOnly><AdminContent /></ProtectedPage>;
}
