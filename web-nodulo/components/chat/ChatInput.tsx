"use client";

import { useCallback, useEffect, useRef, useState, type ChangeEvent, type ClipboardEvent, type FormEvent, type KeyboardEvent } from "react";
import { ArrowUp, BrainCircuit, FileText, Image as ImageIcon, Paperclip, Square, X } from "lucide-react";
import type { ChatUploadLimits } from "@/lib/types";
import { InfoModal } from "@/components/ui/InfoModal";

const allowedExtensions = new Set(["pdf", "txt", "md", "docx", "pptx", "png", "jpg", "jpeg", "webp"]);
const pastedImageExtensions: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/webp": "webp",
};

type LimitNotice = { title: string; message: string };

interface ChatInputProps {
  isStreaming: boolean;
  disabled?: boolean;
  minimal?: boolean;
  attachmentLimits: ChatUploadLimits;
  thinking: boolean;
  onThinkingChange: (enabled: boolean) => void;
  onSend: (prompt: string, files: File[], thinking: boolean) => void;
  onStop: () => void;
}

function formatFileSize(size: number): string {
  return size < 1024 * 1024 ? `${Math.max(1, Math.round(size / 1024))} KB` : `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function isImageFile(file: File): boolean {
  return ["png", "jpg", "jpeg", "webp"].includes(file.name.split(".").pop()?.toLowerCase() ?? "");
}

function ImageAttachmentPreview({ file, onRemove }: { file: File; onRemove: () => void }) {
  const [previewURL, setPreviewURL] = useState("");
  useEffect(() => {
    const objectURL = URL.createObjectURL(file);
    setPreviewURL(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [file]);

  return <div className="composer-image-preview" title={file.name}>
    {previewURL ? <img src={previewURL} alt={`Prévia de ${file.name}`} /> : <ImageIcon size={18} aria-hidden="true" />}
    <span className="composer-image-name">{file.name}</span>
    <button type="button" className="composer-image-remove" onClick={onRemove} aria-label={`Remover ${file.name}`} title={`Remover ${file.name}`}><X size={13} /></button>
  </div>;
}

export function ChatInput({ isStreaming, disabled = false, minimal = false, attachmentLimits, thinking, onThinkingChange, onSend, onStop }: ChatInputProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [attachments, setAttachments] = useState<File[]>([]);
  const [attachmentError, setAttachmentError] = useState<string | null>(null);
  const [limitNotice, setLimitNotice] = useState<LimitNotice | null>(null);
  const closeLimitNotice = useCallback(() => setLimitNotice(null), []);

  function resizeTextarea() {
    const textarea = textareaRef.current;
    if (!textarea) return;
    textarea.style.height = "auto";
    textarea.style.height = `${Math.min(textarea.scrollHeight, 192)}px`;
  }

  function addFiles(selected: File[]) {
    setAttachmentError(null);
    if (selected.length === 0) return;

    const next = [...attachments];
    for (const file of selected) {
      const extension = file.name.split(".").pop()?.toLowerCase() ?? "";
      if (!allowedExtensions.has(extension)) {
        setAttachmentError(`“${file.name}” não é compatível. Anexe PDF, DOCX, PPTX, TXT, MD, PNG, JPEG ou WebP.`);
        continue;
      }
      if (attachmentLimits.max_file_size_mib > 0 && file.size > attachmentLimits.max_file_size_mib * 1024 * 1024) {
        setLimitNotice({
          title: "Arquivo acima do limite configurado",
          message: `“${file.name}” tem ${formatFileSize(file.size)}. Nesta instalação, cada arquivo pode ter até ${attachmentLimits.max_file_size_mib.toLocaleString("pt-BR")} MiB. O administrador pode alterar esse valor ou definir 0 para não impor limite de tamanho por arquivo.`,
        });
        continue;
      }
      if (attachmentLimits.max_files > 0 && next.length >= attachmentLimits.max_files) {
        setLimitNotice({
          title: "Quantidade de arquivos acima do limite",
          message: `Esta instalação permite até ${attachmentLimits.max_files.toLocaleString("pt-BR")} arquivos por mensagem. O administrador pode aumentar o limite ou definir 0 para permitir qualquer quantidade.`,
        });
        break;
      }
      const nextTotalBytes = next.reduce((sum, item) => sum + item.size, file.size);
      if (attachmentLimits.max_total_size_mib > 0 && nextTotalBytes > attachmentLimits.max_total_size_mib * 1024 * 1024) {
        setLimitNotice({
          title: "Tamanho total acima do limite configurado",
          message: `Os arquivos selecionados totalizam ${formatFileSize(nextTotalBytes)}. Nesta instalação, o total por mensagem pode ser de até ${attachmentLimits.max_total_size_mib.toLocaleString("pt-BR")} MiB. O administrador pode alterar esse valor ou definir 0 para não impor limite total.`,
        });
        continue;
      }
      next.push(file);
    }
    setAttachments(next);
  }

  function handleFileSelection(event: ChangeEvent<HTMLInputElement>) {
    const selected = Array.from(event.currentTarget.files ?? []);
    event.currentTarget.value = "";
    addFiles(selected);
  }

  function handlePaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const clipboardItems = Array.from(event.clipboardData.items);
    const imageItems = clipboardItems.filter((item) => item.kind === "file" && item.type.toLowerCase().startsWith("image/"));
    if (imageItems.length === 0) return;

    event.preventDefault();
    const files: File[] = [];
    const unsupportedTypes: string[] = [];
    for (const item of imageItems) {
      const mimeType = item.type.toLowerCase();
      const extension = pastedImageExtensions[mimeType];
      const file = item.getAsFile();
      if (!extension || !file) {
        unsupportedTypes.push(mimeType.replace("image/", "").toUpperCase() || "desconhecido");
        continue;
      }
      const nameMatchesType = extension === "jpg"
        ? /\.(jpe?g)$/i.test(file.name)
        : file.name.toLowerCase().endsWith(`.${extension}`);
      const name = nameMatchesType ? file.name : `imagem-colada-${Date.now()}-${files.length + 1}.${extension}`;
      files.push(nameMatchesType ? file : new File([file], name, { type: mimeType, lastModified: Date.now() }));
    }

    addFiles(files);
    if (unsupportedTypes.length > 0) {
      setAttachmentError(`O formato ${unsupportedTypes.join(", ")} não é aceito. Cole imagens PNG, JPEG ou WebP.`);
    }
    if (files.length === 0 && unsupportedTypes.length === 0) {
      setAttachmentError("Não foi possível ler a imagem da área de transferência. Tente copiá-la novamente ou use o botão de anexo.");
    }
  }

  function removeAttachment(index: number) {
    setAttachments((current) => current.filter((_, itemIndex) => itemIndex !== index));
    setAttachmentError(null);
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isStreaming) {
      onStop();
      return;
    }
    if (disabled) return;
    const input = textareaRef.current;
    const typedPrompt = input?.value.trim() ?? "";
    if (!typedPrompt && attachments.length === 0) return;
    onSend(typedPrompt, [...attachments], thinking);
    if (input) input.value = "";
    setAttachments([]);
    setAttachmentError(null);
    resizeTextarea();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      event.currentTarget.form?.requestSubmit();
    }
  }

  return (
    <div className={`composer-shell ${minimal ? "composer-shell-minimal" : ""}`}>
      <form onSubmit={submit} className="composer-box">
        {attachments.length > 0 && (
          <div className="composer-attachments" aria-label="Arquivos anexados">
            {attachments.map((file, index) => isImageFile(file)
              ? <ImageAttachmentPreview key={`${file.name}-${file.lastModified}-${index}`} file={file} onRemove={() => removeAttachment(index)} />
              : <div className="composer-attachment" key={`${file.name}-${file.lastModified}-${index}`}>
                  <FileText size={15} aria-hidden="true" />
                  <span className="composer-attachment-name" title={file.name}>{file.name}</span>
                  <span className="composer-attachment-size">{formatFileSize(file.size)}</span>
                  <button type="button" className="composer-attachment-remove" onClick={() => removeAttachment(index)} aria-label={`Remover ${file.name}`} title={`Remover ${file.name}`}>
                    <X size={14} />
                  </button>
                </div>)}
          </div>
        )}
        <div className="composer-entry-row">
          <label className="sr-only" htmlFor="chat-prompt">Mensagem para o NÓDULO</label>
          <textarea
            id="chat-prompt"
            ref={textareaRef}
            rows={1}
            maxLength={20000}
            disabled={disabled}
            placeholder="Pergunte, projete ou depure algo…"
            onInput={resizeTextarea}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            className="composer-textarea max-h-48 min-h-7 flex-1 resize-none bg-transparent px-1 py-1.5 text-[14px] leading-6 text-zinc-100 outline-none placeholder:text-zinc-600"
          />
          <input
            ref={fileInputRef}
            className="sr-only"
            type="file"
            multiple
            accept=".pdf,.docx,.pptx,.txt,.md,.png,.jpg,.jpeg,.webp,image/png,image/jpeg,image/webp"
            onChange={handleFileSelection}
            tabIndex={-1}
          />
          <button
            type="button"
            className="composer-attach-button"
            aria-label="Anexar arquivos ou colar uma imagem"
            title="Anexar arquivos ou colar uma imagem"
            disabled={disabled || isStreaming}
            onClick={() => fileInputRef.current?.click()}
          >
            <Paperclip size={18} />
          </button>
          <button
            type="button"
            className={`composer-thinking-button ${thinking ? "composer-thinking-button-active" : ""}`}
            aria-label={thinking ? "Desativar modo Pensar" : "Ativar modo Pensar"}
            aria-pressed={thinking}
            title={thinking ? "Pensar ativado: resposta mais analítica" : "Pensar: solicitar uma resposta mais analítica"}
            disabled={disabled || isStreaming}
            onClick={() => onThinkingChange(!thinking)}
          >
            <BrainCircuit size={15} aria-hidden="true" />
            <span>Pensar</span>
          </button>
          <button
            type={isStreaming ? "button" : "submit"}
            onClick={isStreaming ? onStop : undefined}
            aria-label={isStreaming ? "Parar geração" : "Enviar mensagem"}
            title={isStreaming ? "Parar geração" : "Enviar mensagem"}
            disabled={disabled && !isStreaming}
            className={`send-button ${isStreaming ? "send-button-stop" : ""}`}
          >
            {isStreaming ? <Square size={13} fill="currentColor" /> : <ArrowUp size={17} strokeWidth={2.2} />}
          </button>
        </div>
      </form>
      {attachmentError && <p className="composer-attachment-error" role="alert">{attachmentError}</p>}
      <InfoModal open={limitNotice !== null} title={limitNotice?.title ?? "Limite de anexos"} onClose={closeLimitNotice}>
        <p>{limitNotice?.message}</p>
        <p>Os limites são configurados pela administração para esta instalação. Arquivos aceitos: PDF, DOCX, PPTX, TXT, MD, PNG, JPEG e WebP. As imagens serão analisadas pelo modelo visual configurado na instalação.</p>
      </InfoModal>
      {!minimal && (
        <>
          <p className="mt-3 text-center text-[10px] text-zinc-600">
            O NÓDULO pode cometer erros. Verifique implementações críticas.
          </p>
        </>
      )}
    </div>
  );
}
