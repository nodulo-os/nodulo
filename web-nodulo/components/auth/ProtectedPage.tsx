"use client";

import { useEffect, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { LoaderCircle, LockKeyhole } from "lucide-react";
import { useAuth } from "@/context/AuthContext";

export function ProtectedPage({ children, adminOnly = false }: { children: ReactNode; adminOnly?: boolean }) {
  const { isLoading, isAuthenticated, user, homeForUser } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (isLoading) return;
    if (!isAuthenticated) router.replace("/login");
    else if (adminOnly && user?.role !== "admin") router.replace(homeForUser(user));
  }, [adminOnly, homeForUser, isAuthenticated, isLoading, router, user]);

  if (isLoading) {
    return (
      <main className="access-check-screen" aria-busy="true">
        <section className="access-check-card" role="status" aria-live="polite">
          <div className="access-check-brand" aria-label="NÓDULO">
            <span className="access-check-brand-mark" aria-hidden="true">N</span>
            <span className="access-check-brand-name">NÓDULO</span>
          </div>

          <div className="access-check-orbit" aria-hidden="true">
            <span className="access-check-orbit-ring" />
            <span className="access-check-orbit-core">N</span>
          </div>

          <p className="access-check-eyebrow">ACESSO À PLATAFORMA</p>
          <h1>Verificando seu acesso</h1>
          <p className="access-check-description">Estamos confirmando sua sessão para preparar seu espaço.</p>

          <div className="access-check-progress" aria-hidden="true"><span /></div>
          <div className="access-check-status">
            <LoaderCircle size={14} className="access-check-spinner" aria-hidden="true" />
            <span>Isso deve levar apenas um instante</span>
          </div>
        </section>

        <div className="access-check-footer">
          <LockKeyhole size={13} aria-hidden="true" />
          <span>Verificação de sessão</span>
        </div>
      </main>
    );
  }

  // A navegação de saída já está a caminho; não apresente um estado de verificação
  // para quem não tem sessão nem durante o redirecionamento de acesso negado.
  if (!isAuthenticated || (adminOnly && user?.role !== "admin")) return null;

  return children;
}
