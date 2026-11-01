"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { Eye, EyeOff, KeyRound, LoaderCircle, LogIn, Mail } from "lucide-react";
import { AuthFrame } from "@/components/auth/AuthFrame";
import { useAuth } from "@/context/AuthContext";
import { userFacingError } from "@/lib/errors";

export default function LoginPage() {
  const { login, isAuthenticated, isLoading, homeForUser, user } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isLoading && isAuthenticated) window.location.replace(homeForUser(user));
  }, [homeForUser, isAuthenticated, isLoading, user]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (!email.trim() || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setError("Informe um endereço de e-mail válido.");
      return;
    }
    if (!password) {
      setError("Informe sua senha.");
      return;
    }
    setIsSubmitting(true);
    try {
      await login(email.trim(), password);
    } catch (cause) {
      setError(userFacingError(cause, "Não foi possível entrar na conta. Verifique os dados e tente novamente."));
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthFrame title="Acesse sua conta" description="Entre no NÓDULO e use IA sob suas próprias regras.">
      <form onSubmit={handleSubmit} className="auth-form-fields space-y-3" noValidate>
        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-zinc-300">E-mail</span>
          <span className="relative block"><Mail size={16} className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-zinc-500" /><input type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} placeholder="voce@email.com" className="auth-input h-11 w-full rounded-xl border border-white/10 bg-black/20 pl-10 pr-3 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-emerald-300/45 focus:ring-2 focus:ring-emerald-300/10" /></span>
        </label>
        <label className="block">
          <span className="mb-1.5 block text-xs font-medium text-zinc-300">Senha</span>
          <span className="relative block"><KeyRound size={16} className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-zinc-500" /><input type={showPassword ? "text" : "password"} autoComplete="current-password" required value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Sua senha" className="auth-input h-11 w-full rounded-xl border border-white/10 bg-black/20 pl-10 pr-11 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-emerald-300/45 focus:ring-2 focus:ring-emerald-300/10" /><button type="button" onClick={() => setShowPassword((show) => !show)} className="absolute right-3 top-1/2 grid size-8 -translate-y-1/2 place-items-center rounded-lg text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200" aria-label={showPassword ? "Ocultar senha" : "Mostrar senha"}>{showPassword ? <EyeOff size={16} /> : <Eye size={16} />}</button></span>
        </label>
        {error && <div role="alert" className="rounded-xl border border-rose-400/15 bg-rose-400/[0.06] px-3.5 py-3 text-xs leading-5 text-rose-200">{error}</div>}
        <button type="submit" disabled={isSubmitting || isLoading} className="auth-submit mt-1 inline-flex h-11 w-full items-center justify-center gap-2 rounded-xl bg-emerald-300 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-60">
          {isSubmitting ? <><LoaderCircle size={16} className="animate-spin" /> Entrando...</> : <><LogIn size={16} /> Entrar</>}
        </button>
      </form>
      <p className="auth-link-footer mt-6 border-t border-white/[0.07] pt-5 text-center text-sm text-zinc-400">Não tem uma conta? <Link href="/register" className="font-medium text-emerald-300 hover:text-emerald-200">Crie aqui</Link></p>
    </AuthFrame>
  );
}
