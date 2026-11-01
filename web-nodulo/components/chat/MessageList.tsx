"use client";

import { useEffect, useRef } from "react";
import type { ChatMessage } from "@/lib/types";
import { MessageItem } from "@/components/chat/MessageItem";

interface MessageListProps {
  messages: ChatMessage[];
  busy?: boolean;
  onRetryMessage: (message: ChatMessage, prompt: string) => void;
}

export function MessageList({ messages, busy = false, onRetryMessage }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages]);

  return (
    <div className="space-y-8 pb-8">
      {messages.map((message, index) => <MessageItem key={message.id} message={message} hasFollowingMessages={index < messages.length - 1} busy={busy} onRetryMessage={onRetryMessage} />)}
      <div ref={bottomRef} />
    </div>
  );
}
