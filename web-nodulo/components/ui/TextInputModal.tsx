"use client";

import { useEffect, useRef, useState } from "react";
import { FolderPen, LoaderCircle, X } from "lucide-react";

type TextInputModalProps = {
  open: boolean;
  title: string;
  label: string;
  initialValue: string;
  placeholder?: string;
  error?: string | null;
  busy?: boolean;
  submitLabel: string;
  onSubmit: (value: string) => void | Promise<void>;
  onCancel: () => void;
};

export function TextInputModal({ open, title, label, initialValue, placeholder, error, busy = false, submitLabel, onSubmit, onCancel }: TextInputModalProps) {
  const [value, setValue] = useState(initialValue);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setValue(initialValue);
    const frame = requestAnimationFrame(() => inputRef.current?.focus());
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) onCancel();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [busy, initialValue, onCancel, open]);

  if (!open) return null;

  return (
    <div className="conversation-confirm-overlay" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onCancel();
    }}>
      <form className="conversation-confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="text-modal-title" onSubmit={(event) => { event.preventDefault(); if (value.trim() && !busy) void onSubmit(value.trim()); }}>
        <button type="button" className="conversation-confirm-close" aria-label="Fechar" disabled={busy} onClick={onCancel}><X size={17} /></button>
        <span className="conversation-confirm-icon"><FolderPen size={19} /></span>
        <p className="conversation-confirm-eyebrow">Organização</p>
        <h2 id="text-modal-title">{title}</h2>
        <label className="conversation-confirm-input-label" htmlFor="text-modal-input">{label}</label>
        <input ref={inputRef} id="text-modal-input" className="conversation-confirm-input" value={value} onChange={(event) => setValue(event.target.value)} placeholder={placeholder} maxLength={80} disabled={busy} />
        {error && <p role="alert" className="conversation-confirm-error">{error}</p>}
        <div className="conversation-confirm-actions">
          <button type="button" className="conversation-confirm-cancel" disabled={busy} onClick={onCancel}>Cancelar</button>
          <button type="submit" className="conversation-confirm-submit" disabled={busy || !value.trim()}>{busy && <LoaderCircle size={14} className="animate-spin" />}{busy ? "Salvando…" : submitLabel}</button>
        </div>
      </form>
    </div>
  );
}
