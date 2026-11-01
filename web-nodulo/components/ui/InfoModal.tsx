"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { Info, X } from "lucide-react";

export function InfoModal({ open, title, children, onClose }: {
  open: boolean;
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => closeRef.current?.focus());
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose, open]);

  if (!open) return null;
  return (
    <div className="conversation-confirm-overlay" onMouseDown={(event) => {
      if (event.target === event.currentTarget) onClose();
    }}>
      <section className="conversation-confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="attachment-limit-title">
        <button ref={closeRef} type="button" className="conversation-confirm-close" aria-label="Fechar aviso" onClick={onClose}><X size={17} /></button>
        <span className="conversation-confirm-icon"><Info size={19} /></span>
        <p className="conversation-confirm-eyebrow">Configuração de anexos</p>
        <h2 id="attachment-limit-title">{title}</h2>
        <div className="confirm-modal-description">{children}</div>
        <div className="conversation-confirm-actions">
          <button type="button" className="conversation-confirm-submit" onClick={onClose}>Entendi</button>
        </div>
      </section>
    </div>
  );
}
