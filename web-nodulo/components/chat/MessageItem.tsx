"use client";

import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { AlertTriangle, Check, CheckCircle2, ChevronDown, ExternalLink, FileText, Image as ImageIcon, LoaderCircle, Pencil, RotateCcw, Search, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import type { ChatMessage, ChatSource, MessageAttachment, ResearchProgress } from "@/lib/types";
import { CodeBlock } from "@/components/chat/CodeBlock";

interface MessageItemProps {
  message: ChatMessage;
  hasFollowingMessages?: boolean;
  busy?: boolean;
  onRetryMessage: (message: ChatMessage, prompt: string) => void;
}

const markdownComponents: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ children, className }) => {
    const code = String(children);
    const language = /language-([^\s]+)/.exec(className ?? "")?.[1];
    const isBlock = Boolean(language) || code.includes("\n");
    if (isBlock) return <CodeBlock code={code} language={language} />;
    return <code className="inline-code">{children}</code>;
  },
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer" className="markdown-link">
      {children}
    </a>
  ),
};

function formatSize(size: number): string {
  if (size < 1024 * 1024) return `${Math.max(1, Math.round(size / 1024))} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function AttachmentCards({ attachments }: { attachments: MessageAttachment[] }) {
  if (attachments.length === 0) return null;
  return (
    <div className="message-attachments" aria-label="Arquivos enviados nesta mensagem">
      {attachments.map((attachment, index) => {
        const extension = attachment.name.split(".").pop()?.toUpperCase() ?? "ARQUIVO";
        const isImage = ["PNG", "JPG", "JPEG", "WEBP"].includes(extension);
        return (
          <div className="message-attachment-card" key={`${attachment.name}-${index}`}>
            <span className="message-attachment-icon">{isImage ? <ImageIcon size={18} aria-hidden="true" /> : <FileText size={18} aria-hidden="true" />}</span>
            <span className="message-attachment-details">
              <span className="message-attachment-name" title={attachment.name}>{attachment.name}</span>
              <span className="message-attachment-meta">{extension} <span aria-hidden="true">·</span> {formatSize(attachment.size)}</span>
            </span>
          </div>
        );
      })}
    </div>
  );
}

function ResearchTrace({ steps, sources, isStreaming }: { steps: ResearchProgress[]; sources: ChatSource[]; isStreaming: boolean }) {
  if (steps.length === 0 && sources.length === 0) return null;
  const visibleSteps = steps.filter((step) => step.action !== "planning");

  return <details className="research-trace">
    <summary>
      <Search size={14} aria-hidden="true" />
      <span>{isStreaming ? "Pesquisando" : "Acompanhar pesquisa"}</span>
      {(visibleSteps.length > 0 || sources.length > 0) && <span className="research-trace-count">· {sources.length > 0 ? `${sources.length} fontes` : `${visibleSteps.length} etapas`}</span>}
      {isStreaming && <LoaderCircle size={13} className="animate-spin research-trace-spinner" aria-label="Pesquisa em andamento" />}
      <ChevronDown size={13} className="research-trace-chevron" aria-hidden="true" />
    </summary>
    <div className="research-trace-content">
      <p className="research-trace-note">Etapas e fontes consultadas.</p>
      {sources.length > 0 && <div className="research-trace-sources" aria-label="Fontes consultadas">
        {sources.map((source, index) => source.url
          ? <a className="research-trace-source" href={source.url} key={`${source.url}-${index}`} target="_blank" rel="noreferrer">{source.title || source.url}<ExternalLink size={11} /></a>
          : <span className="research-trace-source" key={`${source.source_reference}-${source.title}-${index}`}>{source.title || source.source_reference || "Acervo institucional"}</span>)}
      </div>}
      {visibleSteps.length > 0 && <ol>
        {visibleSteps.map((step, index) => <li className={`research-trace-step research-trace-${step.status}`} key={`${step.action}-${step.status}-${step.step}-${index}`}>
          <span className="research-trace-status" aria-hidden="true">
            {step.status === "started" ? <LoaderCircle size={13} className="animate-spin" /> :
              step.status === "completed" ? <CheckCircle2 size={13} /> :
                step.status === "blocked" || step.status === "failed" ? <AlertTriangle size={13} /> : <Search size={13} />}
          </span>
          <span className="research-trace-detail">
            <span className="research-trace-message">{step.message}</span>
            {step.action === "search_web" && step.input && <span className="research-trace-input">Consulta: {step.input}</span>}
            {step.action === "read_page" && step.input && <span className="research-trace-input">Página consultada: {step.input}</span>}
            {step.sources?.map((source) => <a className="research-trace-source" href={source.url} key={`${index}-${source.url}`} target="_blank" rel="noreferrer">{source.title || source.url}<ExternalLink size={11} /></a>)}
          </span>
        </li>)}
      </ol>}
    </div>
  </details>;
}

export function MessageItem({ message, hasFollowingMessages = false, busy = false, onRetryMessage }: MessageItemProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(message.content);
  const isAssistant = message.role === "assistant";
  const hasUnavailableAttachments = Boolean(message.attachments?.length && !message.attachmentFiles?.length);

  function submitEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || (!draft.trim() && !message.attachmentFiles?.length)) return;
    setEditing(false);
    onRetryMessage(message, draft.trim());
  }

  return (
    <article className={`message-row ${isAssistant ? "message-assistant" : "message-user"}`}>
      <div className="min-w-0 flex-1">
        {!isAssistant && <AttachmentCards attachments={message.attachments ?? []} />}
        {editing ? <form className="message-edit-form" onSubmit={submitEdit}>
          <label className="sr-only" htmlFor={`edit-${message.id}`}>Editar mensagem</label>
          <textarea id={`edit-${message.id}`} autoFocus value={draft} onChange={(event) => setDraft(event.target.value)} maxLength={20000} className="message-edit-textarea" />
          {hasFollowingMessages && <p className="message-edit-note">Ao enviar, esta mensagem será substituída e as mensagens posteriores serão removidas da conversa.</p>}
          <div className="message-edit-actions">
            <button type="button" onClick={() => { setDraft(message.content); setEditing(false); }} disabled={busy} className="message-action-button"><X size={14} />Cancelar</button>
            <button type="submit" disabled={busy || (!draft.trim() && !message.attachmentFiles?.length)} className="message-edit-submit"><Check size={14} />Enviar</button>
          </div>
        </form> : <div className="markdown-body text-[14px] leading-7 text-zinc-300">
          {isAssistant && (message.sources || message.researchSteps) && <ResearchTrace steps={message.researchSteps ?? []} sources={message.sources ?? []} isStreaming={Boolean(message.isStreaming)} />}
          {message.content ? (
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
              {message.content}
            </ReactMarkdown>
          ) : message.isStreaming ? (
            <span className="inline-flex items-center gap-2 text-sm text-zinc-500">
              <span className="thinking-dots"><i /><i /><i /></span>
              <span>Organizando o raciocínio</span>
            </span>
          ) : null}
          {message.generatedImages?.map((image, index) => <figure className="generated-chart" key={`${message.id}-chart-${index}`}>
            <img src={image} alt={`Gráfico gerado pelo NÓDULO${message.generatedImages!.length > 1 ? ` ${index + 1}` : ""}`} loading="lazy" />
            <figcaption>Gráfico gerado a partir do cálculo executado.</figcaption>
          </figure>)}
          {message.isStreaming && message.content.length > 0 && <span className="stream-cursor" aria-label="Gerando resposta" />}
        </div>}
        {!isAssistant && !editing && <div className="message-actions">
          <button type="button" onClick={() => onRetryMessage(message, message.content)} disabled={busy || hasUnavailableAttachments} title={hasUnavailableAttachments ? "Anexe novamente os arquivos desta mensagem para reenviar" : "Reenviar mensagem"} aria-label="Reenviar mensagem" className="message-action-button"><RotateCcw size={14} /><span>Reenviar</span></button>
          <button type="button" onClick={() => { setDraft(message.content); setEditing(true); }} disabled={busy || hasUnavailableAttachments} title={hasUnavailableAttachments ? "Anexe novamente os arquivos desta mensagem para editá-la" : "Editar mensagem"} aria-label="Editar mensagem" className="message-action-button"><Pencil size={14} /><span>Editar</span></button>
        </div>}
      </div>
    </article>
  );
}
