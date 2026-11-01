import { ChatContainer } from "@/components/chat/ChatContainer";
import { ProtectedPage } from "@/components/auth/ProtectedPage";

export default function ChatPage() {
  return <ProtectedPage><ChatContainer /></ProtectedPage>;
}
