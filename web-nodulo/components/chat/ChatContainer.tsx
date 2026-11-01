"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { LoaderCircle } from "lucide-react";
import { useAppTheme } from "@/context/ThemeContext";
import { useChatStream } from "@/hooks/useChatStream";
import { ChatInput } from "@/components/chat/ChatInput";
import { MessageList } from "@/components/chat/MessageList";
import { Header } from "@/components/layout/Header";
import { Sidebar } from "@/components/layout/Sidebar";

export function ChatContainer() {
  const { theme, toggleTheme } = useAppTheme();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [thinkingEnabled, setThinkingEnabled] = useState(false);
  const urlSyncPaused = useRef(true);
  const {
    messages,
    isStreaming,
    error,
    currentConversationId,
    conversationRefreshVersion,
    isLoadingConversation,
    attachmentLimits,
    sendMessage,
    stopGeneration,
    loadConversation,
    startNewChat,
  } = useChatStream();

  // A conversa ativa fica no query param para que recarregar ou copiar o link
  // preserve a seleção sem desmontar o estado do chat durante o streaming.
  useEffect(() => {
    const restoreFromURL = () => {
      const conversationId = new URLSearchParams(window.location.search).get("conversation");
      if (conversationId) {
        urlSyncPaused.current = true;
        void loadConversation(conversationId).finally(() => { urlSyncPaused.current = false; });
      } else {
        urlSyncPaused.current = false;
        startNewChat();
      }
    };
    restoreFromURL();
    window.addEventListener("popstate", restoreFromURL);
    return () => window.removeEventListener("popstate", restoreFromURL);
  }, [loadConversation, startNewChat]);

  useEffect(() => {
    if (urlSyncPaused.current) return;
    const url = new URL(window.location.href);
    if (currentConversationId) url.searchParams.set("conversation", currentConversationId);
    else url.searchParams.delete("conversation");
    const nextURL = `${url.pathname}${url.search}${url.hash}`;
    const currentURL = `${window.location.pathname}${window.location.search}${window.location.hash}`;
    if (nextURL !== currentURL) window.history.replaceState(window.history.state, "", nextURL);
  }, [currentConversationId]);

  const handleLoadConversation = useCallback((conversationId: string) => {
    const url = new URL(window.location.href);
    url.searchParams.set("conversation", conversationId);
    window.history.pushState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
    urlSyncPaused.current = false;
    void loadConversation(conversationId);
  }, [loadConversation]);

  const handleNewChat = useCallback(() => {
    const url = new URL(window.location.href);
    url.searchParams.delete("conversation");
    window.history.pushState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
    urlSyncPaused.current = false;
    startNewChat();
  }, [startNewChat]);

  const isEmpty = messages.length === 0;

  return (
    <div className="chat-workspace flex h-dvh overflow-hidden" data-theme={theme}>
      <Sidebar
        collapsed={sidebarCollapsed}
        mobileOpen={mobileSidebarOpen}
        currentConversationId={currentConversationId}
        conversationRefreshVersion={conversationRefreshVersion}
        isLoadingConversation={isLoadingConversation}
        onToggleSidebar={() => setSidebarCollapsed((value) => !value)}
        onCloseMobile={() => setMobileSidebarOpen(false)}
        onNewChat={handleNewChat}
        onLoadConversation={handleLoadConversation}
      />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header
          theme={theme}
          onToggleTheme={toggleTheme}
          onOpenMobileSidebar={() => setMobileSidebarOpen(true)}
        />
        <main className="chat-main relative flex min-h-0 flex-1 flex-col">
          <div className={`chat-scroll-area ${isEmpty || isLoadingConversation ? "chat-scroll-empty" : ""}`}>
            {isLoadingConversation ? (
              <div className="conversation-loading" role="status">
                <LoaderCircle size={17} className="animate-spin" />
                <span>Carregando conversa</span>
              </div>
            ) : isEmpty ? (
              <div className="welcome-panel">
                <h1 className="welcome-title">O que vamos construir?</h1>
                <p className="welcome-copy">Pense, projete e resolva problemas</p>
                {error && <div role="status" className="welcome-error">{error}</div>}
                <ChatInput
                  isStreaming={isStreaming}
                  disabled={isLoadingConversation}
                  minimal
                  attachmentLimits={attachmentLimits}
                  thinking={thinkingEnabled}
                  onThinkingChange={setThinkingEnabled}
                  onSend={(prompt, files, thinking) => { void sendMessage(prompt, files, undefined, thinking); }}
                  onStop={stopGeneration}
                />
              </div>
            ) : (
              <div className="message-list-wrap">
                <MessageList
                  messages={messages}
                  busy={isStreaming || isLoadingConversation}
                  onRetryMessage={(message, prompt) => { void sendMessage(prompt, message.attachmentFiles ?? [], message, thinkingEnabled); }}
                />
              </div>
            )}
          </div>
          {!isEmpty && (
            <div className="composer-dock">
              {error && (
                <div role="status" className="mx-auto mb-3 max-w-3xl rounded-lg border border-rose-400/15 bg-rose-400/[0.05] px-3 py-2 text-xs text-rose-200/80">
                  {error}
                </div>
              )}
              <ChatInput isStreaming={isStreaming} disabled={isLoadingConversation} attachmentLimits={attachmentLimits} thinking={thinkingEnabled} onThinkingChange={setThinkingEnabled} onSend={(prompt, files, thinking) => { void sendMessage(prompt, files, undefined, thinking); }} onStop={stopGeneration} />
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
