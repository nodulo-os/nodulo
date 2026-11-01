"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { AlertTriangle, LoaderCircle, X } from "lucide-react";

type ConfirmModalProps = {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  busy?: boolean;
  onConfirm: () => void | Promise<void>;
  onCancel: () => void;
};

export function ConfirmModal({ open, title, description, confirmLabel, busy = false, onConfirm, onCancel }: ConfirmModalProps) {
  const confirmRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => confirmRef.current?.focus());
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) onCancel();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [busy, onCancel, open]);

  if (!open) return null;

  return (
    <div className="conversation-confirm-overlay" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !busy) onCancel();
    }}>
      <section className="conversation-confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-modal-title" aria-describedby="confirm-modal-description">
        <button type="button" className="conversation-confirm-close" aria-label="Fechar confirmação" disabled={busy} onClick={onCancel}><X size={17} /></button>
        <span className="conversation-confirm-icon conversation-confirm-icon-danger"><AlertTriangle size={19} /></span>
        <p className="conversation-confirm-eyebrow">Confirmação necessária</p>
        <h2 id="confirm-modal-title">{title}</h2>
        <div id="confirm-modal-description" className="confirm-modal-description">{description}</div>
        <div className="conversation-confirm-actions">
          <button type="button" className="conversation-confirm-cancel" disabled={busy} onClick={onCancel}>Cancelar</button>
          <button ref={confirmRef} type="button" className="conversation-confirm-submit conversation-confirm-submit-danger" disabled={busy} onClick={() => void onConfirm()}>
            {busy && <LoaderCircle size={14} className="animate-spin" />}{busy ? "Aguarde…" : confirmLabel}
          </button>
        </div>
      </section>
    </div>
  );
}
