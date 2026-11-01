"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { AlertTriangle, Archive, Atom, Building2, Check, ChevronDown, Ellipsis, Folder, LoaderCircle, PanelLeftClose, PanelLeftOpen, Pencil, Pin, PinOff, Plus, Trash2, X } from "lucide-react";
import type { ConversationFolder, PersistedConversation } from "@/lib/types";
import { apiURL } from "@/lib/api";
import { apiErrorMessage, userFacingError } from "@/lib/errors";
import { useAuth } from "@/context/AuthContext";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { TextInputModal } from "@/components/ui/TextInputModal";
import { useInstitutionSettings } from "@/hooks/useInstitutionSettings";
const PINNED_STORAGE_KEY = "nodulo:pinned-conversations:v1";

interface SidebarProps {
  collapsed: boolean;
  mobileOpen: boolean;
  currentConversationId: string | null;
  conversationRefreshVersion: number;
  isLoadingConversation: boolean;
  onToggleSidebar: () => void;
  onCloseMobile: () => void;
  onNewChat: () => void;
  onLoadConversation: (conversationId: string) => void;
}

type ConversationGroup = {
  title: "Hoje" | "Ontem" | "Anteriores";
  conversations: PersistedConversation[];
};

type PendingConversationAction = {
  kind: "archive" | "delete";
  conversation: PersistedConversation;
};

function groupConversations(conversations: PersistedConversation[]): ConversationGroup[] {
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  const groups = new Map<ConversationGroup["title"], PersistedConversation[]>([
    ["Hoje", []],
    ["Ontem", []],
    ["Anteriores", []],
  ]);

  [...conversations]
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .forEach((conversation) => {
      const updatedAt = new Date(conversation.updated_at || conversation.created_at);
      const day = new Date(updatedAt.getFullYear(), updatedAt.getMonth(), updatedAt.getDate());
      const group = day >= today ? "Hoje" : day >= yesterday ? "Ontem" : "Anteriores";
      groups.get(group)?.push(conversation);
    });

  return [...groups].map(([title, items]) => ({ title, conversations: items })).filter((group) => group.conversations.length > 0);
}

export function Sidebar({
  collapsed,
  mobileOpen,
  currentConversationId,
  conversationRefreshVersion,
  isLoadingConversation,
  onToggleSidebar,
  onCloseMobile,
  onNewChat,
  onLoadConversation,
}: SidebarProps) {
  const { token, authorizedFetch } = useAuth();
  const institution = useInstitutionSettings();
  const [conversations, setConversations] = useState<PersistedConversation[]>([]);
  const [folders, setFolders] = useState<ConversationFolder[]>([]);
  const [isLoadingList, setIsLoadingList] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [folderFeedback, setFolderFeedback] = useState<string | null>(null);
  const [folderError, setFolderError] = useState<string | null>(null);
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [folderBusy, setFolderBusy] = useState(false);
  const [folderMenuID, setFolderMenuID] = useState<string | null>(null);
  const [folderToRename, setFolderToRename] = useState<ConversationFolder | null>(null);
  const [folderToDelete, setFolderToDelete] = useState<ConversationFolder | null>(null);
  const [expandedFolders, setExpandedFolders] = useState<Record<string, boolean>>({});
  const [foldersSectionOpen, setFoldersSectionOpen] = useState(true);
  const [pinnedIDs, setPinnedIDs] = useState<string[]>([]);
  const [pinsReady, setPinsReady] = useState(false);
  const [pinnedSectionOpen, setPinnedSectionOpen] = useState(true);
  const [recentSectionOpen, setRecentSectionOpen] = useState(true);
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  const [pendingConversationAction, setPendingConversationAction] = useState<PendingConversationAction | null>(null);
  const [conversationActionBusy, setConversationActionBusy] = useState(false);
  const [conversationActionError, setConversationActionError] = useState<string | null>(null);
  const confirmActionRef = useRef<HTMLButtonElement>(null);
  const listRequestRef = useRef(0);
  const newFolderInputRef = useRef<HTMLInputElement>(null);
  const isCollapsed = collapsed && !mobileOpen;
  const pinnedIDSet = useMemo(() => new Set(pinnedIDs), [pinnedIDs]);
  const pinnedConversations = useMemo(
    () => conversations
      .filter((conversation) => pinnedIDSet.has(conversation.id))
      .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)),
    [conversations, pinnedIDSet],
  );
  const folderConversations = useCallback((folderID: string) => conversations
    .filter((conversation) => conversation.folder_id === folderID && !pinnedIDSet.has(conversation.id))
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)), [conversations, pinnedIDSet]);
  const recentGroups = useMemo(
    () => groupConversations(conversations.filter((conversation) => !pinnedIDSet.has(conversation.id) && !conversation.folder_id)),
    [conversations, pinnedIDSet],
  );
  const recentConversationCount = recentGroups.reduce((count, group) => count + group.conversations.length, 0);

  useEffect(() => {
    try {
      const savedIDs: unknown = JSON.parse(localStorage.getItem(PINNED_STORAGE_KEY) ?? "[]");
      if (Array.isArray(savedIDs) && savedIDs.every((id) => typeof id === "string")) {
        setPinnedIDs(savedIDs);
      }
    } catch {
      try {
        localStorage.removeItem(PINNED_STORAGE_KEY);
      } catch {
        // O armazenamento pode estar desativado pelo navegador.
      }
    } finally {
      setPinsReady(true);
    }
  }, []);

  useEffect(() => {
    if (!pinsReady) return;
    try {
      localStorage.setItem(PINNED_STORAGE_KEY, JSON.stringify(pinnedIDs));
    } catch {
      // A lista continua funcional durante a sessão se o armazenamento estiver indisponível.
    }
  }, [pinsReady, pinnedIDs]);

  const fetchFolders = useCallback(async () => {
    if (!token) {
      setFolders([]);
      return;
    }
    try {
      const response = await authorizedFetch(apiURL("/api/conversation-folders"));
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível carregar as áreas de trabalho. Tente novamente."));
      const result = await response.json() as ConversationFolder[];
      setFolders(Array.isArray(result) ? result : []);
      setFolderError(null);
    } catch (cause) {
      setFolderError(userFacingError(cause, "Não foi possível carregar as áreas de trabalho. Verifique sua conexão e tente novamente."));
    }
  }, [authorizedFetch, token]);

  useEffect(() => { void fetchFolders(); }, [fetchFolders]);

  useEffect(() => {
    const controller = new AbortController();
    const requestId = ++listRequestRef.current;
    setIsLoadingList(true);
    setListError(null);

    if (!token) {
      setConversations([]);
      setIsLoadingList(false);
      return () => controller.abort();
    }

    authorizedFetch(apiURL("/api/conversations"), { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(await apiErrorMessage(response, "Não foi possível carregar suas conversas. Atualize a página e tente novamente."));
        }
        return response.json() as Promise<PersistedConversation[]>;
      })
      .then((items) => {
        if (listRequestRef.current === requestId) setConversations(items);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted || listRequestRef.current !== requestId) return;
        setListError(userFacingError(cause, "Não foi possível carregar suas conversas. Verifique sua conexão e tente novamente."));
      })
      .finally(() => {
        if (listRequestRef.current === requestId) setIsLoadingList(false);
      });

    return () => {
      controller.abort();
      if (listRequestRef.current === requestId) listRequestRef.current += 1;
    };
  }, [authorizedFetch, conversationRefreshVersion, token]);

  const togglePinned = (conversationID: string) => {
    setPinnedIDs((current) => current.includes(conversationID)
      ? current.filter((id) => id !== conversationID)
      : [...current, conversationID]);
    setOpenMenuID(null);
  };

  const createFolder = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const name = newFolderName.trim();
    if (!name || folderBusy) return;
    setFolderBusy(true);
    setFolderError(null);
    try {
      const response = await authorizedFetch(apiURL("/api/conversation-folders"), {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }),
      });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível criar a área de trabalho. Tente novamente."));
      const folder = await response.json() as ConversationFolder;
      setFolders((current) => [...current, folder].sort((a, b) => a.name.localeCompare(b.name, "pt-BR")));
      setExpandedFolders((current) => ({ ...current, [folder.id]: true }));
      setNewFolderName("");
      setNewFolderOpen(false);
      setFolderFeedback(`Área de trabalho “${folder.name}” criada.`);
      window.setTimeout(() => setFolderFeedback(null), 3000);
    } catch (cause) {
      setFolderError(userFacingError(cause, "Não foi possível criar a área de trabalho. Confira os dados e tente novamente."));
    } finally { setFolderBusy(false); }
  };

  const renameFolder = (folder: ConversationFolder) => {
    setFolderError(null);
    setFolderMenuID(null);
    setFolderToRename(folder);
  };

  const saveFolderRename = async (proposedName: string) => {
    if (!folderToRename) return;
    const folder = folderToRename;
    const name = proposedName.trim();
    if (!name) return;
    if (name === folder.name) { setFolderToRename(null); return; }
    setFolderBusy(true);
    setFolderError(null);
    try {
      const response = await authorizedFetch(apiURL(`/api/conversation-folders/${encodeURIComponent(folder.id)}`), {
        method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }),
      });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível renomear a área de trabalho. Tente novamente."));
      const updated = await response.json() as ConversationFolder;
      setFolders((current) => current.map((item) => item.id === folder.id ? updated : item).sort((a, b) => a.name.localeCompare(b.name, "pt-BR")));
      setFolderMenuID(null);
      setFolderToRename(null);
      setFolderFeedback(`Área de trabalho renomeada para “${updated.name}”.`);
      window.setTimeout(() => setFolderFeedback(null), 3000);
    } catch (cause) {
      setFolderError(userFacingError(cause, "Não foi possível renomear a área de trabalho. Tente novamente."));
    } finally { setFolderBusy(false); }
  };

  const deleteFolder = async (folder: ConversationFolder) => {
    setFolderBusy(true);
    setFolderError(null);
    try {
      const response = await authorizedFetch(apiURL(`/api/conversation-folders/${encodeURIComponent(folder.id)}`), { method: "DELETE" });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível excluir a área de trabalho. Tente novamente."));
      setFolders((current) => current.filter((item) => item.id !== folder.id));
      setConversations((current) => current.map((conversation) => conversation.folder_id === folder.id ? { ...conversation, folder_id: null } : conversation));
      setFolderMenuID(null);
      setFolderFeedback(`Área de trabalho “${folder.name}” excluída; as conversas foram mantidas.`);
      window.setTimeout(() => setFolderFeedback(null), 3500);
    } catch (cause) {
      setFolderError(userFacingError(cause, "Não foi possível excluir a área de trabalho. Verifique sua conexão e tente novamente."));
    } finally { setFolderBusy(false); }
  };

  const moveConversation = async (conversation: PersistedConversation, folderID: string | null) => {
    setFolderBusy(true);
    setFolderError(null);
    try {
      const response = await authorizedFetch(apiURL(`/api/conversations/${encodeURIComponent(conversation.id)}/folder`), {
        method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ folder_id: folderID }),
      });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível mover a conversa. Tente novamente."));
      setConversations((current) => current.map((item) => item.id === conversation.id ? { ...item, folder_id: folderID } : item));
      setOpenMenuID(null);
      setFolderFeedback(folderID ? "Conversa movida para a área de trabalho." : "Conversa movida para Recentes.");
      window.setTimeout(() => setFolderFeedback(null), 3000);
    } catch (cause) {
      setFolderError(userFacingError(cause, "Não foi possível mover a conversa. Tente novamente."));
    } finally { setFolderBusy(false); }
  };

  useEffect(() => {
    if (!pendingConversationAction) return;
    const frame = requestAnimationFrame(() => confirmActionRef.current?.focus());
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !conversationActionBusy) {
        setPendingConversationAction(null);
        setConversationActionError(null);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [conversationActionBusy, pendingConversationAction]);

  const confirmConversationAction = async () => {
    if (!pendingConversationAction || conversationActionBusy) return;
    const { kind, conversation } = pendingConversationAction;
    setConversationActionBusy(true);
    setConversationActionError(null);
    try {
      const response = kind === "archive"
        ? await authorizedFetch(apiURL(`/api/conversations/${encodeURIComponent(conversation.id)}/archive`), {
          method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ archived: true }),
        })
        : await authorizedFetch(apiURL(`/api/conversations/${encodeURIComponent(conversation.id)}`), { method: "DELETE" });
      if (!response.ok) throw new Error(await apiErrorMessage(response, "Não foi possível concluir a ação na conversa. Tente novamente."));
      setConversations((current) => current.filter((item) => item.id !== conversation.id));
      setPinnedIDs((current) => current.filter((id) => id !== conversation.id));
      if (currentConversationId === conversation.id) onNewChat();
      setPendingConversationAction(null);
    } catch (cause) {
      setConversationActionError(userFacingError(cause, "Não foi possível concluir a ação na conversa. Tente novamente."));
    } finally { setConversationActionBusy(false); }
  };

  const confirmFolderDelete = async () => {
    if (!folderToDelete || folderBusy) return;
    const folder = folderToDelete;
    await deleteFolder(folder);
    setFolderToDelete(null);
  };

  const renderConversation = (conversation: PersistedConversation) => {
    const isActive = currentConversationId === conversation.id;
    const isOpening = isLoadingConversation && isActive;
    const isPinned = pinnedIDSet.has(conversation.id);

    return (
      <div className="history-conversation" key={conversation.id}>
        <button
          type="button"
          onClick={() => {
            setOpenMenuID(null);
            onLoadConversation(conversation.id);
            onCloseMobile();
          }}
          aria-current={isActive ? "page" : undefined}
          className={`history-item ${isActive ? "history-item-active" : ""}`}
          title={conversation.title}
        >
          <span className="truncate">{conversation.title || "Conversa sem título"}</span>
        </button>
        <div className="history-actions">
          {isOpening ? (
            <LoaderCircle size={15} className="animate-spin text-[#557663]" aria-label="Abrindo conversa" />
          ) : (
            <button
              type="button"
              className="history-menu-trigger"
              aria-label={`Opções da conversa: ${conversation.title || "Conversa sem título"}`}
              aria-haspopup="menu"
              aria-expanded={openMenuID === conversation.id}
              onClick={() => setOpenMenuID((current) => current === conversation.id ? null : conversation.id)}
            >
              <Ellipsis size={17} />
            </button>
          )}
        </div>
        {openMenuID === conversation.id && (
          <div className="history-menu" role="menu">
            <button type="button" role="menuitem" onClick={() => togglePinned(conversation.id)}>
              {isPinned ? <PinOff size={15} /> : <Pin size={15} />}
              <span>{isPinned ? "Desafixar conversa" : "Fixar conversa"}</span>
            </button>
            <button type="button" role="menuitem" onClick={() => {
              setPendingConversationAction({ kind: "archive", conversation });
              setConversationActionError(null);
              setOpenMenuID(null);
            }}>
              <Archive size={15} /><span>Arquivar conversa</span>
            </button>
            <div className="history-menu-divider" />
            <p className="history-menu-label">Mover para</p>
            <button type="button" role="menuitem" disabled={folderBusy || !conversation.folder_id} onClick={() => void moveConversation(conversation, null)}>
              <span>Recentes</span>{!conversation.folder_id && <Check size={14} className="ml-auto" />}
            </button>
            {folders.map((folder) => (
              <button key={folder.id} type="button" role="menuitem" disabled={folderBusy || conversation.folder_id === folder.id} onClick={() => void moveConversation(conversation, folder.id)}>
                <span className="min-w-0 flex-1 truncate">{folder.name}</span>{conversation.folder_id === folder.id && <Check size={14} className="ml-auto" />}
              </button>
            ))}
            {folders.length === 0 && <p className="history-menu-hint">Crie uma área de trabalho para organizar esta conversa.</p>}
            <div className="history-menu-divider" />
            <button type="button" role="menuitem" className="history-menu-danger" onClick={() => {
              setPendingConversationAction({ kind: "delete", conversation });
              setConversationActionError(null);
              setOpenMenuID(null);
            }}>
              <Trash2 size={15} /><span>Excluir conversa</span>
            </button>
          </div>
        )}
      </div>
    );
  };

  return (
    <>
      {mobileOpen && <button type="button" className="sidebar-scrim md:hidden" onClick={onCloseMobile} aria-label="Fechar menu" />}
      <aside className={`sidebar ${isCollapsed ? "sidebar-collapsed" : ""} ${mobileOpen ? "sidebar-mobile-open" : ""}`}>
        <div className={`sidebar-header ${isCollapsed ? "sidebar-header-collapsed" : ""}`}>
          <div className="sidebar-brand flex items-center gap-2.5 overflow-hidden">
            <div className="brand-mark"><Atom size={18} strokeWidth={1.7} /></div>
            {!isCollapsed && <span className="text-[14px] font-semibold tracking-[0.1em] text-zinc-100">NÓDULO</span>}
          </div>
          <button
            type="button"
            onClick={() => mobileOpen ? onCloseMobile() : onToggleSidebar()}
            className="sidebar-toggle-button"
            aria-label={mobileOpen ? "Fechar barra lateral" : isCollapsed ? "Expandir barra lateral" : "Recolher barra lateral"}
            title={mobileOpen ? "Fechar barra lateral" : isCollapsed ? "Expandir barra lateral" : "Recolher barra lateral"}
          >
            {isCollapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}
          </button>
        </div>

        <div className="px-3 pt-3">
          <button type="button" onClick={() => { onNewChat(); onCloseMobile(); }} className={`new-chat-button ${isCollapsed ? "justify-center px-0" : ""}`} title="Nova conversa">
            <Plus size={16} strokeWidth={2} />
            {!isCollapsed && <span>Nova conversa</span>}
            {!isCollapsed && <kbd className="ml-auto">⌘ K</kbd>}
          </button>
        </div>

        {!isCollapsed && (
          <div className="sidebar-nav px-3 pt-5">
            <section className="conversation-section mb-4">
              <div className="conversation-section-heading">
                <button
                  type="button"
                  className="conversation-section-toggle"
                  aria-expanded={foldersSectionOpen}
                  onClick={() => setFoldersSectionOpen((open) => !open)}
                >
                  <ChevronDown size={15} className={foldersSectionOpen ? "" : "section-chevron-collapsed"} />
                  <span>Áreas de trabalho</span>
                  {folders.length > 0 && <span className="conversation-count">{folders.length}</span>}
                </button>
                <button
                  type="button"
                  className="area-create-trigger"
                  aria-label="Criar área de trabalho"
                  title="Criar área de trabalho"
                  onClick={() => {
                    setFoldersSectionOpen(true);
                    setNewFolderOpen(true);
                    setFolderError(null);
                    requestAnimationFrame(() => newFolderInputRef.current?.focus());
                  }}
                >Criar</button>
              </div>
              {foldersSectionOpen && <>
              {newFolderOpen && (
                <form className="folder-create-form" onSubmit={(event) => void createFolder(event)}>
                  <input
                    ref={newFolderInputRef}
                    maxLength={100}
                    value={newFolderName}
                    onChange={(event) => setNewFolderName(event.target.value)}
                    placeholder="Nome da área de trabalho"
                    aria-label="Nome da nova área de trabalho"
                    disabled={folderBusy}
                  />
                  <button type="submit" aria-label="Salvar área de trabalho" disabled={folderBusy || !newFolderName.trim()}><Check size={14} /></button>
                  <button type="button" aria-label="Cancelar criação" disabled={folderBusy} onClick={() => { setNewFolderOpen(false); setNewFolderName(""); }}><X size={14} /></button>
                </form>
              )}
              {folderError && <p className="folder-feedback folder-feedback-error" role="alert">{folderError}</p>}
              {folderFeedback && <p className="folder-feedback" role="status">{folderFeedback}</p>}
              {folders.length > 0 && (
                <div className="conversation-section-content mt-1">
                  {folders.map((folder) => {
                    const items = folderConversations(folder.id);
                    const isOpen = expandedFolders[folder.id] ?? false;
                    return (
                      <div className="conversation-section folder-group" key={folder.id}>
                        <div className="folder-heading">
                          <button type="button" className="folder-heading-toggle" aria-expanded={isOpen} onClick={() => setExpandedFolders((current) => ({ ...current, [folder.id]: !isOpen }))}>
                            <ChevronDown size={14} className={isOpen ? "" : "section-chevron-collapsed"} />
                            <Folder size={14} />
                            <span className="min-w-0 flex-1 truncate" title={folder.name}>{folder.name}</span>
                            <span className="conversation-count">{items.length}</span>
                          </button>
                          <button type="button" className="folder-menu-trigger" aria-label={`Opções da área de trabalho ${folder.name}`} aria-haspopup="menu" aria-expanded={folderMenuID === folder.id} onClick={() => setFolderMenuID((current) => current === folder.id ? null : folder.id)}><Ellipsis size={16} /></button>
                          {folderMenuID === folder.id && <div className="history-menu folder-menu" role="menu">
                            <button type="button" role="menuitem" disabled={folderBusy} onClick={() => renameFolder(folder)}><Pencil size={14} /><span>Renomear área</span></button>
                            <button type="button" role="menuitem" disabled={folderBusy} onClick={() => { setFolderToDelete(folder); setFolderMenuID(null); setFolderError(null); }}><Trash2 size={14} /><span>Excluir área</span></button>
                          </div>}
                        </div>
                        {isOpen && (items.length > 0
                          ? <div className="folder-conversation-list space-y-0.5">{items.map(renderConversation)}</div>
                          : <p className="conversation-list-message folder-empty">Nenhuma conversa nesta área.</p>)}
                      </div>
                    );
                  })}
                </div>
              )}
              </>}
            </section>

            <section className="conversation-section">
              <button
                type="button"
                className="conversation-section-toggle"
                aria-expanded={pinnedSectionOpen}
                onClick={() => setPinnedSectionOpen((open) => !open)}
              >
                <ChevronDown size={15} className={pinnedSectionOpen ? "" : "section-chevron-collapsed"} />
                <span>Fixados</span>
                {pinnedConversations.length > 0 && <span className="conversation-count">{pinnedConversations.length}</span>}
              </button>
              {pinnedSectionOpen && (
                <div className="conversation-section-content">
                  {pinnedConversations.length > 0 ? (
                    <div className="space-y-0.5">{pinnedConversations.map(renderConversation)}</div>
                  ) : (
                    <p className="conversation-list-message">Fixe conversas pelo menu ⋯ de cada item.</p>
                  )}
                </div>
              )}
            </section>

            <section className="conversation-section mt-4" aria-busy={isLoadingList}>
              <button
                type="button"
                className="conversation-section-toggle"
                aria-expanded={recentSectionOpen}
                onClick={() => setRecentSectionOpen((open) => !open)}
              >
                <ChevronDown size={15} className={recentSectionOpen ? "" : "section-chevron-collapsed"} />
                <span>Recentes</span>
                {isLoadingList && <LoaderCircle size={14} className="ml-auto animate-spin" aria-label="Atualizando conversas" />}
                {!isLoadingList && recentConversationCount > 0 && <span className="conversation-count">{recentConversationCount}</span>}
              </button>
              {recentSectionOpen && (
                <div className="conversation-section-content mt-2">
                  {isLoadingList && conversations.length === 0 ? (
                    <div className="space-y-2 px-2 py-1" aria-label="Carregando conversas">
                      <div className="history-skeleton w-14" />
                      <div className="history-skeleton w-full" />
                      <div className="history-skeleton w-4/5" />
                      <div className="history-skeleton w-3/5" />
                    </div>
                  ) : listError && conversations.length === 0 ? (
                    <p className="conversation-list-message" role="status">Não foi possível carregar o histórico.</p>
                  ) : recentGroups.length === 0 ? (
                    <p className="conversation-list-message">Nenhuma conversa salva ainda.</p>
                  ) : (
                    recentGroups.map((section) => (
                      <div className="mb-4 last:mb-0" key={section.title}>
                        <p className="conversation-date-label">{section.title}</p>
                        <div className="space-y-0.5">
                          {section.conversations.map(renderConversation)}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              )}
            </section>
          </div>
        )}

        {!isCollapsed && institution?.show_institution && institution.institution_name && <div className="mt-auto px-4 pb-4 pt-3"><div className="flex min-w-0 items-start gap-2 border-t border-zinc-200 pt-3 text-zinc-500 dark:border-white/10"><Building2 size={14} className="mt-0.5 shrink-0" /><div className="min-w-0"><p className="truncate text-[11px] font-medium" title={institution.institution_name}>{institution.institution_name}{institution.institution_acronym ? ` · ${institution.institution_acronym}` : ""}</p>{institution.institution_unit && <p className="mt-0.5 truncate text-[10px] opacity-75" title={institution.institution_unit}>{institution.institution_unit}</p>}</div></div></div>}
      </aside>
      {pendingConversationAction && (
        <div
          className="conversation-confirm-overlay"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget && !conversationActionBusy) {
              setPendingConversationAction(null);
              setConversationActionError(null);
            }
          }}
        >
          <section className="conversation-confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="conversation-confirm-title" aria-describedby="conversation-confirm-description">
            <button type="button" className="conversation-confirm-close" aria-label="Fechar confirmação" disabled={conversationActionBusy} onClick={() => { setPendingConversationAction(null); setConversationActionError(null); }}><X size={17} /></button>
            <span className={`conversation-confirm-icon ${pendingConversationAction.kind === "delete" ? "conversation-confirm-icon-danger" : ""}`}>
              {pendingConversationAction.kind === "archive" ? <Archive size={19} /> : <AlertTriangle size={19} />}
            </span>
            <p className="conversation-confirm-eyebrow">{pendingConversationAction.kind === "archive" ? "Organização do histórico" : "Ação permanente"}</p>
            <h2 id="conversation-confirm-title">{pendingConversationAction.kind === "archive" ? "Arquivar esta conversa?" : "Excluir esta conversa?"}</h2>
            <p id="conversation-confirm-description">
              {pendingConversationAction.kind === "archive"
                ? "A conversa será retirada da lista principal e guardada na sua conta. As mensagens não serão apagadas."
                : "A conversa e todas as mensagens serão apagadas permanentemente. Esta ação não pode ser desfeita."}
            </p>
            {conversationActionError && <p role="alert" className="conversation-confirm-error">{conversationActionError}</p>}
            <div className="conversation-confirm-actions">
              <button type="button" className="conversation-confirm-cancel" disabled={conversationActionBusy} onClick={() => { setPendingConversationAction(null); setConversationActionError(null); }}>Cancelar</button>
              <button
                ref={confirmActionRef}
                type="button"
                className={`conversation-confirm-submit ${pendingConversationAction.kind === "delete" ? "conversation-confirm-submit-danger" : ""}`}
                disabled={conversationActionBusy}
                onClick={() => void confirmConversationAction()}
              >
                {conversationActionBusy && <LoaderCircle size={15} className="animate-spin" />}
                {pendingConversationAction.kind === "archive" ? "Arquivar conversa" : "Excluir permanentemente"}
              </button>
            </div>
          </section>
        </div>
      )}
      <ConfirmModal
        open={folderToDelete !== null}
        title="Excluir área de trabalho?"
        description={folderToDelete ? `Excluir a área de trabalho “${folderToDelete.name}”? As conversas serão mantidas em Recentes.` : ""}
        confirmLabel="Excluir área"
        busy={folderBusy}
        onConfirm={confirmFolderDelete}
        onCancel={() => { if (!folderBusy) setFolderToDelete(null); }}
      />
      <TextInputModal
        open={folderToRename !== null}
        title="Renomear área de trabalho"
        label="Nome da área"
        initialValue={folderToRename?.name ?? ""}
        placeholder="Ex.: Redes de computadores"
        error={folderError}
        busy={folderBusy}
        submitLabel="Salvar nome"
        onSubmit={saveFolderRename}
        onCancel={() => { if (!folderBusy) { setFolderToRename(null); setFolderError(null); } }}
      />
    </>
  );
}
