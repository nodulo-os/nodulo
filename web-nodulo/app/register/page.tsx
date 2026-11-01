"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { Check, Eye, EyeOff, LoaderCircle, LockKeyhole, Mail, UserRound, UserPlus } from "lucide-react";
import { AuthFrame } from "@/components/auth/AuthFrame";
import { useAuth } from "@/context/AuthContext";
import { userFacingError } from "@/lib/errors";

export default function RegisterPage() {
  const { register, isAuthenticated, isLoading, homeForUser, user } = useAuth();
  const router = useRouter();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [successMessage, setSuccessMessage] = useState("");

  useEffect(() => {
    if (!isLoading && isAuthenticated) router.replace(homeForUser(user));
  }, [homeForUser, isAuthenticated, isLoading, router, user]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (name.trim().length < 2) return setError("Informe seu nome completo.");
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) return setError("Informe um endereço de e-mail válido.");
    if ([...password].length < 6) return setError("A senha precisa ter pelo menos 6 caracteres.");
    if (password !== confirmation) return setError("As senhas não coincidem.");

    setIsSubmitting(true);
    try {
      const result = await register(name.trim(), email.trim(), password);
      setSuccessMessage(result.message);
      setSuccess(true);
    } catch (cause) {
      setError(userFacingError(cause, "Não foi possível concluir seu cadastro. Confira os dados e tente novamente."));
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthFrame title="Crie sua conta" description="Comece a explorar ideias e resolver problemas com o NÓDULO.">
      {success ? (
        <div role="status" className="rounded-xl border border-emerald-300/15 bg-emerald-300/[0.06] p-4 text-sm text-emerald-100"><div className="flex items-center gap-2 font-medium"><Check size={17} /> Cadastro recebido</div><p className="mt-2 text-xs leading-5 text-zinc-300">{successMessage}</p></div>
      ) : (
        <form onSubmit={handleSubmit} className="auth-form-fields space-y-3" noValidate>
          <label className="block"><span className="mb-1 block text-xs font-medium text-zinc-300">Nome completo</span><span className="relative block"><UserRound size={16} className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-zinc-500" /><input type="text" autoComplete="name" required minLength={2} value={name} onChange={(event) => setName(event.target.value)} placeholder="Seu nome" className="auth-input h-11 w-full rounded-xl border border-white/10 bg-black/20 pl-10 pr-3 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-emerald-300/45 focus:ring-2 focus:ring-emerald-300/10" /></span></label>
          <label className="block"><span className="mb-1 block text-xs font-medium text-zinc-300">E-mail</span><span className="relative block"><Mail size={16} className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-zinc-500" /><input type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} placeholder="voce@email.com" className="auth-input h-11 w-full rounded-xl border border-white/10 bg-black/20 pl-10 pr-3 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-emerald-300/45 focus:ring-2 focus:ring-emerald-300/10" /></span></label>
          <label className="block"><span className="mb-1 block text-xs font-medium text-zinc-300">Senha</span><span className="relative block"><LockKeyhole size={16} className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-zinc-500" /><input type={showPassword ? "text" : "password"} autoComplete="new-password" required value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Pelo menos 6 caracteres" className="auth-input h-11 w-full rounded-xl border border-white/10 bg-black/20 pl-10 pr-11 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-emerald-300/45 focus:ring-2 focus:ring-emerald-300/10" /><button type="button" onClick={() => setShowPassword((show) => !show)} className="absolute right-3 top-1/2 grid size-8 -translate-y-1/2 place-items-center rounded-lg text-zinc-500 hover:bg-white/[0.06] hover:text-zinc-200" aria-label={showPassword ? "Ocultar senha" : "Mostrar senha"}>{showPassword ? <EyeOff size={16} /> : <Eye size={16} />}</button></span></label>
          <label className="block"><span className="mb-1 block text-xs font-medium text-zinc-300">Confirmar senha</span><input type={showPassword ? "text" : "password"} autoComplete="new-password" required value={confirmation} onChange={(event) => setConfirmation(event.target.value)} placeholder="Digite a senha novamente" className="auth-input h-11 w-full rounded-xl border border-white/10 bg-black/20 px-3.5 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-emerald-300/45 focus:ring-2 focus:ring-emerald-300/10" /></label>
          {error && <div role="alert" className="rounded-xl border border-rose-400/15 bg-rose-400/[0.06] px-3.5 py-3 text-xs leading-5 text-rose-200">{error}</div>}
          <button type="submit" disabled={isSubmitting || isLoading} className="auth-submit mt-1 inline-flex h-11 w-full items-center justify-center gap-2 rounded-xl bg-emerald-300 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-200 disabled:cursor-wait disabled:opacity-60">{isSubmitting ? <><LoaderCircle size={16} className="animate-spin" /> Criando conta...</> : <><UserPlus size={16} /> Criar conta</>}</button>
        </form>
      )}
      <p className="auth-link-footer mt-6 border-t border-white/[0.07] pt-5 text-center text-sm text-zinc-400">Já tem uma conta? <Link href="/login" className="font-medium text-emerald-300 hover:text-emerald-200">Entre aqui</Link></p>
    </AuthFrame>
  );
}
