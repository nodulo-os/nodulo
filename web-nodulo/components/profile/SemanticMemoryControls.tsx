"use client";

import { useCallback, useEffect, useState } from "react";
import { Brain, Check, CheckCircle2, LoaderCircle, Trash2, X } from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { apiURL } from "@/lib/api";
import { apiErrorMessage, userFacingError } from "@/lib/errors";

type MemoryFact = { id: string; fact: string; created_at: string };
type MemoryState = { enabled: boolean; facts: MemoryFact[] };

function dateLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Data indisponível" : new Intl.DateTimeFormat("pt-BR", { dateStyle: "medium", timeStyle: "short" }).format(date);
}

export function SemanticMemoryControls() {
  const { authorizedFetch } = useAuth();
  const [memory, setMemory] = useState<MemoryState>({ enabled: false, facts: [] });
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [deletingID, setDeletingID] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [confirmClear, setConfirmClear] = useState(false);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const response = await authorizedFetch(apiURL("/api/memory"), { signal });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível carregar sua memória pessoal."));
      const result = await response.json() as MemoryState;
      setMemory({ enabled: Boolean(result.enabled), facts: Array.isArray(result.facts) ? result.facts : [] });
      setError(null);
    } catch (cause) {
      if (!signal?.aborted) setError(userFacingError(cause, "Não foi possível carregar sua memória pessoal. Verifique sua conexão e tente novamente."));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, [authorizedFetch]);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const setEnabled = async (enabled: boolean) => {
    setBusy(true); setError(null); setFeedback(null);
    try {
      const response = await authorizedFetch(apiURL("/api/memory/settings"), {
        method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ enabled }),
      });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível salvar a configuração."));
      setMemory((current) => ({ ...current, enabled }));
      setFeedback(enabled ? "Memória ativada. Informações duradouras poderão ser guardadas para personalizar respostas." : "Memória desativada. Novos fatos não serão guardados nem usados nas respostas.");
    } catch (cause) {
      setError(userFacingError(cause, "Não foi possível alterar a memória. Tente novamente."));
    } finally { setBusy(false); }
  };

  const deleteFact = async (id: string) => {
    setDeletingID(id); setError(null); setFeedback(null);
    try {
      const response = await authorizedFetch(apiURL(`/api/memory/${encodeURIComponent(id)}`), { method: "DELETE" });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível apagar esta informação."));
      setMemory((current) => ({ ...current, facts: current.facts.filter((item) => item.id !== id) }));
      setFeedback("Informação removida da sua memória pessoal.");
    } catch (cause) { setError(userFacingError(cause, "Não foi possível apagar esta informação. Tente novamente.")); }
    finally { setDeletingID(null); }
  };

  const clearAll = async () => {
    setBusy(true); setError(null);
    try {
      const response = await authorizedFetch(apiURL("/api/memory"), { method: "DELETE" });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível apagar a memória."));
      setMemory((current) => ({ ...current, facts: [] }));
      setFeedback("Todas as informações salvas foram apagadas.");
      setConfirmClear(false);
    } catch (cause) { setError(userFacingError(cause, "Não foi possível apagar a memória. Tente novamente.")); }
    finally { setBusy(false); }
  };

  return (
    <section className="theme-adaptive overflow-hidden rounded-2xl border border-white/[0.08] bg-[#101513]">
      <header className="flex flex-col gap-4 border-b border-white/[0.07] px-5 py-5 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-3"><span className="grid size-10 shrink-0 place-items-center rounded-xl border border-emerald-300/15 bg-emerald-300/[0.06] text-emerald-200"><Brain size={17} /></span><div><h2 className="font-medium text-zinc-100">Memória pessoal</h2><p className="mt-1 max-w-2xl text-xs leading-5 text-zinc-500">Consulte e controle informações que o NÓDULO pode usar para adaptar respostas futuras. Você pode remover qualquer item ou apagar tudo.</p></div></div>
        <button type="button" role="switch" aria-checked={memory.enabled} aria-label="Ativar memória pessoal" disabled={loading || busy} onClick={() => void setEnabled(!memory.enabled)} className={`inline-flex min-h-10 shrink-0 items-center gap-2 self-start rounded-full border px-3 text-xs font-medium transition disabled:cursor-wait disabled:opacity-50 ${memory.enabled ? "border-emerald-300/25 bg-emerald-300/[0.08] text-emerald-100" : "border-white/10 bg-white/[0.025] text-zinc-300"}`}>
          {busy ? <LoaderCircle size={14} className="animate-spin" /> : memory.enabled ? <Check size={14} /> : <span className="size-3 rounded-full border border-current opacity-60" />}{memory.enabled ? "Ativada" : "Desativada"}
        </button>
      </header>
      <div className="px-5 py-4">
        <p className="text-xs leading-5 text-zinc-500">Desativar bloqueia novas gravações e o uso dos fatos. Os itens já salvos permanecem disponíveis abaixo até serem apagados. A memória começa desativada.</p>
        {feedback && <p role="status" className="mt-4 flex items-start gap-2 rounded-lg border border-emerald-300/15 bg-emerald-300/[0.05] px-3 py-2.5 text-xs leading-5 text-emerald-100"><CheckCircle2 size={15} className="mt-0.5 shrink-0" />{feedback}</p>}
        {error && <p role="alert" className="mt-4 rounded-lg border border-rose-300/15 bg-rose-300/[0.05] px-3 py-2.5 text-xs leading-5 text-rose-200">{error}</p>}
        <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div><h3 className="text-sm font-medium text-zinc-200">Informações salvas <span className="ml-1 text-xs font-normal text-zinc-500">{memory.facts.length}</span></h3><p className="mt-1 text-xs text-zinc-500">Cada informação pode ser removida separadamente.</p></div><button type="button" onClick={() => setConfirmClear(true)} disabled={memory.facts.length === 0 || busy} className="inline-flex min-h-9 items-center justify-center gap-2 self-start rounded-lg border border-rose-300/15 px-3 text-xs text-rose-200 transition hover:bg-rose-300/[0.06] disabled:cursor-not-allowed disabled:opacity-40 sm:self-auto"><Trash2 size={13} /> Apagar tudo</button></div>
        {loading ? <div className="mt-4 space-y-2" aria-label="Carregando memória pessoal"><div className="h-16 animate-pulse rounded-xl bg-white/[0.04]" /><div className="h-16 animate-pulse rounded-xl bg-white/[0.04]" /></div> : memory.facts.length === 0 ? <div className="mt-4 rounded-xl border border-dashed border-white/[0.1] px-4 py-8 text-center"><Brain size={19} className="mx-auto text-zinc-600" /><p className="mt-2 text-sm text-zinc-300">Nenhuma informação salva</p><p className="mt-1 text-xs text-zinc-500">{memory.enabled ? "Informações úteis e duradouras aparecerão aqui." : "A memória está desativada e não guarda novas informações."}</p></div> : <ul className="mt-4 divide-y divide-white/[0.06] rounded-xl border border-white/[0.07]">{memory.facts.map((item) => <li key={item.id} className="flex items-start gap-3 px-4 py-3.5"><div className="min-w-0 flex-1"><p className="whitespace-pre-wrap break-words text-sm leading-6 text-zinc-200">{item.fact}</p><p className="mt-1 text-[11px] text-zinc-500">Salva em {dateLabel(item.created_at)}</p></div><button type="button" aria-label="Apagar informação" title="Apagar informação" onClick={() => void deleteFact(item.id)} disabled={deletingID !== null} className="grid size-8 shrink-0 place-items-center rounded-lg text-zinc-500 transition hover:bg-rose-300/[0.07] hover:text-rose-200 disabled:opacity-40">{deletingID === item.id ? <LoaderCircle size={14} className="animate-spin" /> : <X size={15} />}</button></li>)}</ul>}
      </div>
      {confirmClear && <div className="conversation-confirm-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) setConfirmClear(false); }}><section className="conversation-confirm-dialog theme-adaptive" role="alertdialog" aria-modal="true" aria-labelledby="clear-memory-title" aria-describedby="clear-memory-description"><div className="grid size-11 place-items-center rounded-xl border border-rose-300/20 bg-rose-300/[0.07] text-rose-200"><Trash2 size={18} /></div><h2 id="clear-memory-title" className="mt-4 text-lg font-semibold text-zinc-100">Apagar todas as informações?</h2><p id="clear-memory-description" className="mt-2 text-sm leading-6 text-zinc-400">Esta ação removerá permanentemente os {memory.facts.length} itens da sua memória pessoal. Suas conversas não serão apagadas.</p><div className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end"><button type="button" onClick={() => setConfirmClear(false)} disabled={busy} className="min-h-10 rounded-lg border border-white/10 px-4 text-sm text-zinc-300 transition hover:bg-white/[0.05] disabled:opacity-50">Cancelar</button><button type="button" onClick={() => void clearAll()} disabled={busy} className="inline-flex min-h-10 items-center justify-center gap-2 rounded-lg bg-rose-700 px-4 text-sm font-medium text-white transition hover:bg-rose-600 disabled:opacity-60">{busy && <LoaderCircle size={15} className="animate-spin" />}Apagar informações</button></div></section></div>}
    </section>
  );
}
