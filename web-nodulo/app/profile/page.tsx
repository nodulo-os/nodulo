"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import {
  Archive, ArrowLeft, CheckCircle2, Clock3, LoaderCircle, LogOut, Mail,
  RefreshCw, RotateCcw, ShieldCheck, UserRound,
} from "lucide-react";
import { ProtectedPage } from "@/components/auth/ProtectedPage";
import { SemanticMemoryControls } from "@/components/profile/SemanticMemoryControls";
import { useAuth } from "@/context/AuthContext";
import { ThemeToggleButton } from "@/context/ThemeContext";
import { apiURL } from "@/lib/api";
import { apiErrorMessage, userFacingError } from "@/lib/errors";
import type { PersistedConversation } from "@/lib/types";

async function responseMessage(response: Response) {
  return apiErrorMessage(response, "Não foi possível concluir esta ação. Verifique sua conexão e tente novamente.");
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Data indisponível";
  return new Intl.DateTimeFormat("pt-BR", { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function ProfileContent() {
  const { user, logout, authorizedFetch } = useAuth();
  const [archivedConversations, setArchivedConversations] = useState<PersistedConversation[]>([]);
  const [isLoadingArchives, setIsLoadingArchives] = useState(true);
  const [archivesError, setArchivesError] = useState<string | null>(null);
  const [restoringID, setRestoringID] = useState<string | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);

  const loadArchivedConversations = useCallback(async (signal?: AbortSignal) => {
    setIsLoadingArchives(true);
    setArchivesError(null);
    try {
      const response = await authorizedFetch(apiURL("/api/conversations/archived"), { signal });
      if (!response.ok) throw new Error(await responseMessage(response));
      const result = await response.json() as PersistedConversation[];
      setArchivedConversations(Array.isArray(result) ? result : []);
    } catch (cause) {
      if (signal?.aborted) return;
      setArchivesError(userFacingError(cause, "Não foi possível carregar as conversas arquivadas. Verifique sua conexão e tente novamente."));
    } finally {
      if (!signal?.aborted) setIsLoadingArchives(false);
    }
  }, [authorizedFetch]);

  useEffect(() => {
    const controller = new AbortController();
    void loadArchivedConversations(controller.signal);
    return () => controller.abort();
  }, [loadArchivedConversations]);

  const restoreConversation = async (conversation: PersistedConversation) => {
    setRestoringID(conversation.id);
    setFeedback(null);
    setArchivesError(null);
    try {
      const response = await authorizedFetch(apiURL(`/api/conversations/${encodeURIComponent(conversation.id)}/archive`), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ archived: false }),
      });
      if (!response.ok) throw new Error(await responseMessage(response));
      setArchivedConversations((current) => current.filter((item) => item.id !== conversation.id));
      setFeedback(`“${conversation.title || "Conversa sem título"}” foi restaurada para as conversas recentes.`);
    } catch (cause) {
      setArchivesError(userFacingError(cause, "Não foi possível restaurar a conversa. Tente novamente."));
    } finally {
      setRestoringID(null);
    }
  };

  const initial = user?.name.trim().charAt(0).toLocaleUpperCase("pt-BR") || "N";

  return (
    <main className="theme-adaptive min-h-dvh bg-[#0a0d0c] px-4 py-5 text-zinc-100 sm:px-8 sm:py-8">
      <div className="mx-auto max-w-5xl">
        <header className="flex items-center justify-between gap-4 border-b border-white/[0.07] pb-5">
          <Link href="/chat" className="inline-flex min-h-10 items-center gap-2 rounded-lg px-2 text-sm text-zinc-400 transition hover:bg-white/[0.05] hover:text-zinc-100">
            <ArrowLeft size={16} /> Voltar ao chat
          </Link>
          <div className="flex items-center gap-2">
            <ThemeToggleButton />
            <button type="button" onClick={logout} className="inline-flex min-h-10 items-center gap-2 rounded-lg border border-white/10 px-3 text-sm text-zinc-300 transition hover:border-rose-300/20 hover:bg-rose-300/[0.05] hover:text-rose-200">
              <LogOut size={15} /> <span className="hidden sm:inline">Sair da conta</span><span className="sm:hidden">Sair</span>
            </button>
          </div>
        </header>

        <section className="mt-8 grid gap-5 lg:grid-cols-[minmax(250px,.78fr)_minmax(0,1.5fr)] lg:items-start">
          <div className="space-y-5">
            <article className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
              <div className="h-20 bg-gradient-to-r from-emerald-300/[0.13] via-emerald-300/[0.04] to-transparent" />
              <div className="px-5 pb-5">
                <div className="-mt-8 grid size-16 place-items-center rounded-2xl border-4 border-[#101513] bg-[#1b2820] text-xl font-medium text-emerald-100">{initial}</div>
                <p className="mt-4 text-[10px] font-semibold uppercase tracking-[.18em] text-emerald-200/70">Perfil do usuário</p>
                <h1 className="mt-1 break-words text-xl font-semibold tracking-tight text-zinc-50">{user?.name || "Conta"}</h1>
                <p className="mt-1 break-all text-sm text-zinc-400">{user?.email}</p>
                <span className="mt-4 inline-flex items-center gap-1.5 rounded-full border border-emerald-300/15 bg-emerald-300/[0.06] px-2.5 py-1 text-xs text-emerald-100">
                  <ShieldCheck size={13} /> {user?.role === "admin" ? "Administrador" : "Usuário"}
                </span>
              </div>
            </article>

            <article className="rounded-2xl border border-white/[0.08] bg-[#101513] p-5">
              <h2 className="text-sm font-medium text-zinc-100">Informações da conta</h2>
              <div className="mt-4 space-y-4">
                <div className="flex items-start gap-3">
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-white/[0.045] text-zinc-400"><UserRound size={15} /></span>
                  <div className="min-w-0"><p className="text-[11px] text-zinc-500">Nome</p><p className="mt-0.5 break-words text-sm text-zinc-200">{user?.name}</p></div>
                </div>
                <div className="flex items-start gap-3">
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-white/[0.045] text-zinc-400"><Mail size={15} /></span>
                  <div className="min-w-0"><p className="text-[11px] text-zinc-500">E-mail</p><p className="mt-0.5 break-all text-sm text-zinc-200">{user?.email}</p></div>
                </div>
              </div>
              <p className="mt-5 border-t border-white/[0.06] pt-4 text-xs leading-5 text-zinc-500">Os dados do perfil são gerenciados pela instalação do NÓDULO.</p>
            </article>
          </div>

          <section className="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
            <header className="flex flex-col gap-3 border-b border-white/[0.07] px-5 py-5 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-3">
                <span className="grid size-10 shrink-0 place-items-center rounded-xl border border-emerald-300/15 bg-emerald-300/[0.06] text-emerald-200"><Archive size={17} /></span>
                <div><h2 className="font-medium text-zinc-100">Conversas arquivadas</h2><p className="mt-1 text-xs leading-5 text-zinc-500">Conversas guardadas na sua conta, fora da lista principal.</p></div>
              </div>
              <button type="button" onClick={() => void loadArchivedConversations()} disabled={isLoadingArchives} className="inline-flex min-h-9 items-center justify-center gap-2 self-start rounded-lg border border-white/10 px-3 text-xs text-zinc-300 transition hover:bg-white/[0.05] disabled:opacity-50 sm:self-auto">
                <RefreshCw size={13} className={isLoadingArchives ? "animate-spin" : ""} /> Atualizar
              </button>
            </header>

            {feedback && <p role="status" className="mx-5 mt-4 flex items-start gap-2 rounded-lg border border-emerald-300/15 bg-emerald-300/[0.05] px-3 py-2.5 text-xs leading-5 text-emerald-100"><CheckCircle2 size={15} className="mt-0.5 shrink-0" />{feedback}</p>}
            {archivesError && <p role="alert" className="mx-5 mt-4 rounded-lg border border-rose-300/15 bg-rose-300/[0.05] px-3 py-2.5 text-xs leading-5 text-rose-200">{archivesError}</p>}

            {isLoadingArchives ? (
              <div className="space-y-3 p-5" aria-label="Carregando conversas arquivadas">
                <div className="h-[76px] animate-pulse rounded-xl bg-white/[0.04]" />
                <div className="h-[76px] animate-pulse rounded-xl bg-white/[0.04]" />
                <div className="h-[76px] animate-pulse rounded-xl bg-white/[0.04]" />
              </div>
            ) : archivedConversations.length === 0 ? (
              <div className="px-6 py-14 text-center">
                <Archive size={23} className="mx-auto text-zinc-600" />
                <p className="mt-3 text-sm font-medium text-zinc-300">Nenhuma conversa arquivada</p>
                <p className="mt-1 text-xs leading-5 text-zinc-500">Quando arquivar uma conversa, ela ficará guardada aqui.</p>
                <Link href="/chat" className="mt-5 inline-flex min-h-9 items-center gap-2 rounded-lg border border-white/10 px-3 text-xs text-zinc-300 transition hover:bg-white/[0.05]">
                  <ArrowLeft size={13} /> Voltar ao chat
                </Link>
              </div>
            ) : (
              <div className="divide-y divide-white/[0.06]">
                {archivedConversations.map((conversation) => (
                  <article key={conversation.id} className="flex flex-col gap-4 px-5 py-4 transition hover:bg-white/[0.015] sm:flex-row sm:items-center sm:justify-between">
                    <div className="min-w-0">
                      <h3 className="truncate text-sm font-medium text-zinc-200" title={conversation.title}>{conversation.title || "Conversa sem título"}</h3>
                      <p className="mt-1 flex items-center gap-1.5 text-xs text-zinc-500"><Clock3 size={12} /> Arquivada em {formatDate(conversation.archived_at || conversation.updated_at)}</p>
                    </div>
                    <button type="button" onClick={() => void restoreConversation(conversation)} disabled={restoringID !== null} className="inline-flex min-h-9 shrink-0 items-center justify-center gap-2 self-start rounded-lg border border-emerald-300/15 bg-emerald-300/[0.05] px-3 text-xs font-medium text-emerald-100 transition hover:border-emerald-300/25 hover:bg-emerald-300/[0.1] disabled:cursor-wait disabled:opacity-50 sm:self-auto">
                      {restoringID === conversation.id ? <LoaderCircle size={14} className="animate-spin" /> : <RotateCcw size={14} />}
                      Restaurar ao chat
                    </button>
                  </article>
                ))}
              </div>
            )}
          </section>
        </section>
        <div className="mt-5"><SemanticMemoryControls /></div>
      </div>
    </main>
  );
}

export default function ProfilePage() {
  return <ProtectedPage><ProfileContent /></ProtectedPage>;
}
