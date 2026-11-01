"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { ChatMessage, ChatSource, ChatUploadLimits, PersistedMessage, ResearchProgress, StreamChunk } from "@/lib/types";
import { apiURL } from "@/lib/api";
import { useAuth } from "@/context/AuthContext";
import { apiErrorMessage, userFacingError } from "@/lib/errors";

function newId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`;
}

export function useChatStream() {
  const { authorizedFetch } = useAuth();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [currentConversationId, setCurrentConversationId] = useState<string | null>(null);
  const [conversationRefreshVersion, setConversationRefreshVersion] = useState(0);
  const [isLoadingConversation, setIsLoadingConversation] = useState(false);
  const [attachmentLimits, setAttachmentLimits] = useState<ChatUploadLimits>({
    max_files: 30,
    max_file_size_mib: 50,
    max_total_size_mib: 0,
  });
  const controllerRef = useRef<AbortController | null>(null);
  const loadControllerRef = useRef<AbortController | null>(null);
  const conversationIdRef = useRef<string | null>(null);
  const titleRefreshTimerRef = useRef<number | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    authorizedFetch(apiURL("/api/chat/upload-settings"), { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) return;
        setAttachmentLimits(await response.json() as ChatUploadLimits);
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [authorizedFetch]);

  const stopGeneration = useCallback(() => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    setIsStreaming(false);
    setMessages((current) =>
      current.map((message) => ({ ...message, isStreaming: false })),
    );
  }, []);

  const startNewChat = useCallback(() => {
    stopGeneration();
    loadControllerRef.current?.abort();
    loadControllerRef.current = null;
    setIsLoadingConversation(false);
    setMessages([]);
    setError(null);
    conversationIdRef.current = null;
    setCurrentConversationId(null);
  }, [stopGeneration]);

  const loadConversation = useCallback(async (conversationId: string) => {
    stopGeneration();
    loadControllerRef.current?.abort();
    const controller = new AbortController();
    loadControllerRef.current = controller;
    conversationIdRef.current = conversationId;
    setCurrentConversationId(conversationId);
    setMessages([]);
    setIsLoadingConversation(true);
    setError(null);

    try {
      const response = await authorizedFetch(apiURL(`/api/conversations/${encodeURIComponent(conversationId)}/messages`), {
        signal: controller.signal,
      });
      if (!response.ok) {
        throw new Error(await apiErrorMessage(response, "Não foi possível abrir esta conversa. Atualize a página e tente novamente."));
      }
      const persistedMessages = (await response.json()) as PersistedMessage[];
      setMessages(persistedMessages.map((message) => ({
        id: message.id,
        role: message.role,
        content: message.content,
        attachments: message.attachments ?? [],
        generatedImages: message.attachments?.filter((attachment) => attachment.generated && attachment.data_url).map((attachment) => attachment.data_url!) ?? [],
        persisted: true,
        createdAt: new Date(message.created_at),
      })));
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === "AbortError")) {
        setError(userFacingError(cause, "Não foi possível carregar esta conversa. Verifique sua conexão e tente novamente."));
      }
    } finally {
      if (loadControllerRef.current === controller) {
        loadControllerRef.current = null;
        setIsLoadingConversation(false);
      }
    }
  }, [authorizedFetch, stopGeneration]);

  const sendMessage = useCallback(
    async (prompt: string, files: File[] = [], retryTarget?: ChatMessage, thinking = false) => {
      const trimmedPrompt = prompt.trim();
      if ((!trimmedPrompt && files.length === 0) || controllerRef.current || loadControllerRef.current) return;
      const isFirstMessageOfNewConversation = !conversationIdRef.current && !retryTarget;

      const controller = new AbortController();
      controllerRef.current = controller;
      setError(null);
      setIsStreaming(true);

      const userMessage: ChatMessage = {
        id: retryTarget?.id ?? newId(),
        role: "user",
        content: trimmedPrompt,
        attachments: files.map((file) => ({ name: file.name, size: file.size })),
        attachmentFiles: [...files],
        persisted: retryTarget?.persisted ?? false,
        createdAt: new Date(),
      };
      const assistantId = newId();
      const assistantMessage: ChatMessage = {
        id: assistantId,
        role: "assistant",
        content: "",
        createdAt: new Date(),
        isStreaming: true,
      };
      setMessages((current) => {
        if (retryTarget) {
          const targetIndex = current.findIndex((message) => message.id === retryTarget.id);
          if (targetIndex >= 0) return [...current.slice(0, targetIndex), userMessage, assistantMessage];
        }
        return [...current, userMessage, assistantMessage];
      });

      const appendText = (text: string) => {
        setMessages((current) =>
          current.map((message) =>
            message.id === assistantId
              ? { ...message, content: message.content + text }
              : message,
          ),
        );
      };

      try {
        let body: BodyInit;
        let headers: HeadersInit | undefined;
        if (files.length > 0) {
          const formData = new FormData();
          formData.set("prompt", trimmedPrompt);
          formData.set("thinking", String(thinking));
          if (conversationIdRef.current) formData.set("conversation_id", conversationIdRef.current);
          if (retryTarget?.persisted) formData.set("retry_message_id", retryTarget.id);
          for (const file of files) formData.append("files", file, file.name);
          body = formData;
        } else {
          headers = { "Content-Type": "application/json" };
          body = JSON.stringify({
            prompt: trimmedPrompt,
            thinking,
            ...(conversationIdRef.current
              ? { conversation_id: conversationIdRef.current }
              : {}),
            ...(retryTarget?.persisted ? { retry_message_id: retryTarget.id } : {}),
          });
        }
        const response = await authorizedFetch(apiURL("/api/chat"), {
          method: "POST",
          headers,
          body,
          signal: controller.signal,
        });

        if (!response.ok) {
          throw new Error(await apiErrorMessage(response, "Não foi possível enviar sua mensagem. Verifique sua conexão e tente novamente."));
        }
        if (!response.body) throw new Error("A resposta da API não tem stream.");

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        let dataLines: string[] = [];
        let eventName = "message";
        let finished = false;

        const dispatchEvent = () => {
          if (dataLines.length === 0) return;
          const data = dataLines.join("\n");
          dataLines = [];
          const currentEvent = eventName;
          eventName = "message";
          if (data === "[DONE]") {
            finished = true;
            setConversationRefreshVersion((version) => version + 1);
            return;
          }
          try {
            const parsed = JSON.parse(data) as StreamChunk | ChatSource[];
            if (currentEvent === "sources" && Array.isArray(parsed)) {
              const sources = parsed as ChatSource[];
              setMessages((current) => current.map((message) => message.id === assistantId
                ? { ...message, sources }
                : message));
              return;
            }
            const chunk = parsed as StreamChunk;
            if (chunk.error) throw new Error(chunk.error);
            if (currentEvent === "meta" && chunk.conversation_id) {
              conversationIdRef.current = chunk.conversation_id;
              setCurrentConversationId(chunk.conversation_id);
              setConversationRefreshVersion((version) => version + 1);
              if (isFirstMessageOfNewConversation) {
                if (titleRefreshTimerRef.current !== null) window.clearTimeout(titleRefreshTimerRef.current);
                titleRefreshTimerRef.current = window.setTimeout(() => {
                  titleRefreshTimerRef.current = null;
                  setConversationRefreshVersion((version) => version + 1);
                }, 3500);
              }
            }
            if (currentEvent === "meta" && chunk.message_id) {
              setMessages((current) => current.map((message) => message.id === userMessage.id
                ? { ...message, id: chunk.message_id!, persisted: true }
                : message));
            }
            if (currentEvent === "attachment" && chunk.data_url?.startsWith("data:image/png;base64,")) {
              setMessages((current) => current.map((message) => message.id === assistantId
                ? { ...message, generatedImages: [...(message.generatedImages ?? []), chunk.data_url!] }
                : message));
            }
            if (currentEvent === "research" && chunk.action && chunk.status && chunk.message) {
              const progress = chunk as ResearchProgress;
              setMessages((current) => current.map((message) => message.id === assistantId
                ? { ...message, researchSteps: [...(message.researchSteps ?? []), progress] }
                : message));
            }
            if (typeof chunk.text === "string") appendText(chunk.text);
          } catch (cause) {
            if (cause instanceof Error) throw cause;
            throw new Error("Não foi possível interpretar um fragmento da resposta.");
          }
        };

        while (!finished) {
          const { value, done } = await reader.read();
          buffer += decoder.decode(value, { stream: !done });
          const lines = buffer.split(/\r?\n/);
          buffer = lines.pop() ?? "";

          for (const line of lines) {
            if (line === "") {
              dispatchEvent();
              if (finished) break;
            } else if (line.startsWith("data:")) {
              dataLines.push(line.slice(5).trimStart());
            } else if (line.startsWith("event:")) {
              eventName = line.slice(6).trim();
            }
          }
          if (done) {
            if (buffer.startsWith("data:")) dataLines.push(buffer.slice(5).trimStart());
            dispatchEvent();
            break;
          }
        }
        await reader.cancel().catch(() => undefined);
      } catch (cause) {
        if (!(cause instanceof DOMException && cause.name === "AbortError")) {
          const message = userFacingError(cause, "Não foi possível gerar a resposta. Verifique sua conexão e tente novamente.");
          setError(message);
          // Do not render the same failure both as an assistant bubble and as
          // the composer alert. Keep any partial response already received.
          setMessages((current) => current.filter((item) =>
            item.id !== assistantId || item.content.length > 0 || Boolean(item.researchSteps?.length),
          ));
        }
      } finally {
        setMessages((current) =>
          current.map((message) =>
            message.id === assistantId ? { ...message, isStreaming: false } : message,
          ),
        );
        if (controllerRef.current === controller) {
          controllerRef.current = null;
          setIsStreaming(false);
        }
      }
    },
    [authorizedFetch],
  );

  useEffect(() => () => {
    controllerRef.current?.abort();
    loadControllerRef.current?.abort();
    if (titleRefreshTimerRef.current !== null) window.clearTimeout(titleRefreshTimerRef.current);
  }, []);

  return {
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
  };
}
