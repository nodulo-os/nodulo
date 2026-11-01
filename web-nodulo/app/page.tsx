"use client";

import Link from "next/link";
import {
  ArrowDown, ArrowRight, Atom, BrainCircuit, Check, Code2, Database,
  Eye, ExternalLink, LockKeyhole, Search, ServerCog, ShieldCheck,
  Sparkles, UsersRound,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { ThemeToggleButton } from "@/context/ThemeContext";
import { useInstitutionSettings } from "@/hooks/useInstitutionSettings";

const capabilities = [
  {
    icon: Search,
    eyebrow: "INFORMAÇÃO COM ORIGEM",
    title: "Pesquisa aprofundada",
    description: "Encontre informações em diferentes fontes e veja de onde elas vieram para poder conferir por conta própria.",
  },
  {
    icon: Code2,
    eyebrow: "CÁLCULOS E GRÁFICOS",
    title: "Ajuda para analisar dados",
    description: "Faça cálculos, compare informações e crie gráficos para entender melhor seus dados e ideias.",
  },
  {
    icon: Eye,
    eyebrow: "IMAGENS E DOCUMENTOS",
    title: "Entende o que você compartilha",
    description: "Envie imagens, anotações e documentos para receber explicações e explorar o conteúdo com a IA.",
  },
  {
    icon: BrainCircuit,
    eyebrow: "NO SEU RITMO",
    title: "Uma experiência que se adapta",
    description: "Com o tempo, a IA pode considerar suas preferências e aquilo que você compartilha para oferecer ajuda mais adequada.",
  },
];

const principles = [
  {
    number: "01",
    icon: LockKeyhole,
    title: "Soberania e autonomia",
    description: "Escolha onde a IA funciona, quais informações ela pode usar e quem pode acessá-las.",
  },
  {
    number: "02",
    icon: ShieldCheck,
    title: "Dados não são produto",
    description: "O NÓDULO não vende conversas nem documentos. Se a organização conectar um serviço externo, as informações enviadas também ficam sujeitas às regras desse serviço.",
  },
  {
    number: "03",
    icon: UsersRound,
    title: "Feito para diferentes realidades",
    description: "Uma ferramenta aberta para universidades, escolas, empresas, desenvolvedores e uso pessoal.",
  },
];

export default function HomePage() {
  const { user, isAuthenticated, isLoading, homeForUser } = useAuth();
  const institution = useInstitutionSettings();
  const isAdmin = user?.role === "admin";
  const primaryHref = isAuthenticated ? (isAdmin ? "/chat" : homeForUser(user)) : "/register";
  const secondaryHref = isAuthenticated ? (isAdmin ? "/admin" : "/profile") : "/login";

  return (
    <main className="theme-adaptive institutional-page min-h-screen overflow-hidden bg-[#090c0b] text-zinc-100 selection:bg-emerald-300/25 selection:text-emerald-50">
      <div className="pointer-events-none fixed inset-0 -z-0 bg-[radial-gradient(ellipse_at_50%_0%,rgba(16,90,67,.24),transparent_52%)]" />
      <header className="relative z-10 flex h-[76px] w-full items-center justify-between border-b border-white/[0.07] px-4 sm:px-6 lg:px-8 2xl:px-12">
        <Link href="/" className="flex items-center gap-3" aria-label="NÓDULO, página inicial">
          <span className="grid size-9 place-items-center rounded-xl border border-emerald-300/20 bg-emerald-300/[0.08] text-emerald-300"><Atom size={19} strokeWidth={1.7} /></span>
          <span className="text-[15px] font-semibold tracking-[.2em]">NÓDULO</span>
        </Link>
        <div className="hidden items-center gap-2 rounded-full border border-emerald-200/10 bg-emerald-100/[0.04] px-3 py-1.5 text-xs text-zinc-400 sm:flex">
          <span className="relative flex size-2"><span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400/50" /><span className="relative inline-flex size-2 rounded-full bg-emerald-400" /></span>
          Você escolhe como usar · suas informações importam
        </div>
        <nav className="flex items-center gap-2">
          <ThemeToggleButton />
          {!isLoading && isAuthenticated ? (
            <>
              {isAdmin && <Link href="/admin" className="hidden rounded-lg px-3 py-2 text-sm text-zinc-300 hover:bg-white/[0.06] sm:inline-flex">Painel admin</Link>}
              <Link href={isAdmin ? "/chat" : homeForUser(user)} className="inline-flex items-center gap-2 rounded-lg bg-emerald-300 px-3.5 py-2 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-200">Abrir NÓDULO <ArrowRight size={15} /></Link>
            </>
          ) : (
            <Link href="/login" className="inline-flex items-center gap-2 rounded-lg border border-white/10 px-3.5 py-2 text-sm font-medium text-zinc-200 transition hover:border-white/20 hover:bg-white/[0.05]">Entrar <ArrowRight size={15} /></Link>
          )}
        </nav>
      </header>

      <section className="relative z-[1] grid min-h-[650px] w-full items-center gap-12 px-4 py-16 sm:px-6 sm:py-20 lg:min-h-[700px] lg:grid-cols-[1.05fr_.95fr] lg:gap-10 lg:px-8 lg:py-24 2xl:gap-16 2xl:px-12">
        <div className="w-full max-w-[900px]">
          <div className="mb-7 inline-flex items-center gap-2 rounded-full border border-emerald-300/15 bg-emerald-300/[0.055] px-3 py-1.5 font-mono text-[10px] tracking-[.16em] text-emerald-200/90 sm:text-[11px]">
            <Sparkles size={13} /> UMA ALTERNATIVA ABERTA ÀS BIG TECHS
          </div>
          <h1 className="max-w-[960px] text-[clamp(2.7rem,6.2vw,5.35rem)] font-medium leading-[1.02] tracking-[-.065em] text-zinc-50">
            A inteligência artificial pode ser sua.<br /><span className="text-emerald-300">Seus dados também.</span>
          </h1>
          <p className="mt-7 max-w-[760px] text-base leading-8 text-zinc-400 sm:text-lg sm:leading-8">
            O NÓDULO é uma alternativa aberta às grandes plataformas: uma IA para ajudar a pesquisar, criar e resolver problemas, com mais liberdade para decidir onde seus dados ficam e quem pode acessá-los. Suas conversas não são mercadoria: o NÓDULO não as vende nem as transforma em produto.
          </p>
          <div className="mt-9 flex flex-col gap-3 sm:flex-row">
            <Link href={primaryHref} className="inline-flex min-h-12 items-center justify-center gap-2 rounded-xl bg-emerald-300 px-5 text-sm font-semibold text-zinc-950 shadow-[0_8px_30px_rgba(16,185,129,.12)] transition hover:-translate-y-0.5 hover:bg-emerald-200">
              {isAuthenticated ? "Abrir a plataforma" : "Começar a usar"}<ArrowRight size={16} />
            </Link>
            <a href="#recursos" className="inline-flex min-h-12 items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/[0.025] px-5 text-sm font-medium text-zinc-200 transition hover:border-white/20 hover:bg-white/[0.06]">
              Explorar recursos<ArrowDown size={15} />
            </a>
            {!isLoading && isAuthenticated && <Link href={secondaryHref} className="inline-flex min-h-12 items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/[0.025] px-5 text-sm font-medium text-zinc-200 transition hover:border-white/20 hover:bg-white/[0.06]">{isAdmin ? "Painel administrativo" : "Minha conta"}<ExternalLink size={15} /></Link>}
            {!isAuthenticated && <Link href="/login" className="inline-flex min-h-12 items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/[0.025] px-5 text-sm font-medium text-zinc-200 transition hover:border-white/20 hover:bg-white/[0.06]">Entrar na conta</Link>}
          </div>
          <div className="mt-10 flex flex-wrap gap-x-5 gap-y-3 border-t border-white/[0.07] pt-6">
            {["Projeto aberto", "Seus dados sob seu controle", "Para pessoas e organizações", "Sem vender conversas"].map((item) => <span key={item} className="inline-flex items-center gap-2 text-xs text-zinc-400"><Check size={13} className="text-emerald-400" />{item}</span>)}
          </div>
        </div>

        <div className="relative mx-auto w-full max-w-[680px] lg:ml-auto">
          <div className="absolute -inset-8 rounded-[40px] bg-emerald-400/[0.07] blur-3xl" />
          <div className="relative overflow-hidden rounded-[22px] border border-white/10 bg-[#0e1311] shadow-[0_32px_100px_rgba(0,0,0,.45)]">
            <div className="flex items-center justify-between border-b border-white/[0.07] px-5 py-4">
              <div className="flex items-center gap-2.5"><span className="grid size-7 place-items-center rounded-lg bg-emerald-300/10 text-emerald-300"><Atom size={15} /></span><span className="text-xs font-medium text-zinc-300">Uma IA que ajuda de várias formas</span></div>
              <span className="rounded-full border border-emerald-300/15 px-2.5 py-1 font-mono text-[9px] tracking-wider text-emerald-300">NÓDULO</span>
            </div>
            <div className="grid gap-3 p-4 sm:grid-cols-2 sm:p-5">
              {capabilities.map(({ icon: Icon, title, eyebrow }, index) => (
                <div key={title} className="flex min-h-28 gap-3 rounded-xl border border-white/[0.07] bg-white/[0.025] p-4">
                  <span className="grid size-9 shrink-0 place-items-center rounded-lg border border-emerald-300/15 bg-emerald-300/[0.07] text-emerald-300"><Icon size={17} /></span>
                  <div><p className="font-mono text-[8px] tracking-[.12em] text-zinc-500">0{index + 1} · {eyebrow}</p><p className="mt-2 text-sm font-medium text-zinc-200">{title}</p></div>
                </div>
              ))}
            </div>
            <div className="flex items-center gap-3 border-t border-white/[0.07] px-5 py-4 text-xs text-zinc-400"><Database size={15} className="shrink-0 text-emerald-400" /><span>Seu conhecimento, disponível para sua equipe</span><span className="ml-auto size-2 shrink-0 rounded-full bg-emerald-400" /></div>
          </div>
          <div className="absolute -bottom-16 -left-5 hidden items-center gap-3 rounded-xl border border-white/10 bg-[#111714] px-4 py-3 shadow-xl sm:flex"><span className="grid size-8 place-items-center rounded-lg bg-emerald-300/9 text-emerald-300"><ServerCog size={17} /></span><span><span className="block text-[11px] font-medium text-zinc-200">Você escolhe as regras</span><span className="block font-mono text-[9px] text-zinc-500">LIBERDADE · AUTONOMIA · CONTROLE</span></span></div>
        </div>
      </section>

      <section id="recursos" className="relative z-[1] border-y border-white/[0.07] bg-white/[0.015]">
        <div className="w-full px-4 py-16 sm:px-6 sm:py-20 lg:px-8 lg:py-24 2xl:px-12">
          <div className="mb-10 max-w-3xl">
            <p className="font-mono text-[10px] tracking-[.2em] text-emerald-300">UMA IA PARA O DIA A DIA</p>
            <h2 className="mt-3 text-3xl font-medium tracking-[-.04em] text-zinc-100 sm:text-4xl">Ajuda para pensar, aprender e fazer.</h2>
            <p className="mt-4 text-sm leading-7 text-zinc-400 sm:text-base">Converse, pesquise, faça perguntas sobre seus materiais e trabalhe com informações — sem abrir mão da liberdade de escolher como a tecnologia é usada.</p>
          </div>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {capabilities.map(({ icon: Icon, eyebrow, title, description }) => (
              <article key={title} className="group rounded-2xl border border-white/[0.08] bg-[#0d1210] p-5 transition duration-200 hover:-translate-y-1 hover:border-emerald-300/20 hover:bg-[#101714] sm:p-6">
                <span className="grid size-11 place-items-center rounded-xl border border-emerald-200/10 bg-emerald-200/[0.045] text-emerald-300"><Icon size={20} strokeWidth={1.6} /></span>
                <p className="mt-7 font-mono text-[9px] tracking-[.14em] text-emerald-300/70">{eyebrow}</p>
                <h3 className="mt-2 text-lg font-medium tracking-tight text-zinc-100">{title}</h3>
                <p className="mt-3 text-sm leading-6 text-zinc-400">{description}</p>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section id="arquitetura" className="relative z-[1] w-full px-4 py-16 sm:px-6 sm:py-20 lg:px-8 lg:py-24 2xl:px-12">
        <div className="mb-10 max-w-3xl"><p className="font-mono text-[10px] tracking-[.2em] text-emerald-300">LIBERDADE PARA ESCOLHER</p><h2 className="mt-3 text-3xl font-medium tracking-[-.04em] text-zinc-100 sm:text-4xl">Sua tecnologia. Suas regras.</h2><p className="mt-4 text-sm leading-7 text-zinc-400 sm:text-base">O NÓDULO dá a pessoas e organizações mais escolha sobre onde a IA funciona, quais informações ela pode usar e quem tem acesso.</p></div>
        <div className="grid gap-4 lg:grid-cols-2">
          <article className="flex gap-4 rounded-2xl border border-white/[0.08] bg-[#0d1210] p-6 sm:p-8"><span className="grid size-11 shrink-0 place-items-center rounded-xl border border-emerald-200/10 bg-emerald-200/[0.045] text-emerald-300"><Database size={20} /></span><div><h3 className="text-lg font-medium text-zinc-100">Seus materiais, a serviço da sua equipe</h3><p className="mt-3 text-sm leading-6 text-zinc-400">Organize documentos, orientações e conhecimentos importantes para que as pessoas encontrem respostas usando as informações da própria organização.</p></div></article>
          <article className="flex gap-4 rounded-2xl border border-white/[0.08] bg-[#0d1210] p-6 sm:p-8"><span className="grid size-11 shrink-0 place-items-center rounded-xl border border-emerald-200/10 bg-emerald-200/[0.045] text-emerald-300"><Atom size={20} /></span><div><h3 className="text-lg font-medium text-zinc-100">Você decide como usar</h3><p className="mt-3 text-sm leading-6 text-zinc-400">Escolha onde a IA funciona e quem pode usá-la. Ajuste as opções à realidade da sua casa, escola, universidade ou empresa.</p></div></article>
        </div>
      </section>

      <section className="relative z-[1] border-y border-white/[0.07] bg-white/[0.015]">
        <div className="w-full px-4 py-16 sm:px-6 sm:py-20 lg:px-8 lg:py-24 2xl:px-12">
          <div className="mb-10 flex flex-col justify-between gap-5 sm:flex-row sm:items-end">
            <div><p className="font-mono text-[10px] tracking-[.2em] text-emerald-300">POR QUE O NÓDULO EXISTE</p><h2 className="mt-3 text-3xl font-medium tracking-[-.04em] text-zinc-100 sm:text-4xl">A tecnologia deve servir às pessoas.</h2></div>
            <p className="max-w-xl text-sm leading-6 text-zinc-500">Uma alternativa aberta ao domínio das grandes plataformas, que concentra poder e transforma dados em negócio.</p>
          </div>
          <div className="grid gap-4 md:grid-cols-3">
            {principles.map(({ number, icon: Icon, title, description }) => (
              <article key={number} className="group rounded-2xl border border-white/[0.08] bg-[#0d1210] p-6 transition duration-200 hover:-translate-y-1 hover:border-emerald-300/20 hover:bg-[#101714] sm:p-7">
                <div className="mb-8 flex items-start justify-between"><span className="grid size-11 place-items-center rounded-xl border border-emerald-200/10 bg-emerald-200/[0.045] text-emerald-300"><Icon size={20} strokeWidth={1.6} /></span><span className="font-mono text-[10px] text-zinc-600">{number}</span></div>
                <h3 className="text-lg font-medium tracking-tight text-zinc-100">{title}</h3>
                <p className="mt-3 text-sm leading-6 text-zinc-400">{description}</p>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section className="relative z-[1] flex w-full flex-col items-start justify-between gap-8 px-4 py-14 sm:flex-row sm:items-center sm:px-6 sm:py-16 lg:px-8 lg:py-20 2xl:px-12">
        <div><div className="mb-3 flex items-center gap-2 font-mono text-[10px] tracking-[.17em] text-emerald-300"><Sparkles size={13} /> UMA ALTERNATIVA ABERTA</div><h2 className="max-w-2xl text-2xl font-medium tracking-[-.04em] text-zinc-100 sm:text-3xl">A IA deve ampliar sua autonomia.</h2><p className="mt-3 max-w-xl text-sm leading-6 text-zinc-400">Use a tecnologia com mais liberdade, mantenha suas escolhas nas suas mãos e não transforme seus dados em mercadoria.</p></div>
        <Link href={primaryHref} className="inline-flex min-h-12 shrink-0 items-center gap-2 rounded-xl bg-emerald-300 px-5 text-sm font-semibold text-zinc-950 transition hover:bg-emerald-200">{isAuthenticated ? "Acessar a plataforma" : "Começar a usar"}<ArrowRight size={16} /></Link>
      </section>

      <footer className="relative z-[1] border-t border-white/[0.07]">
        <div className="flex w-full flex-col gap-3 px-4 py-6 text-xs text-zinc-500 sm:flex-row sm:items-center sm:justify-between sm:px-6 lg:px-8 2xl:px-12">
          <div className="flex flex-wrap items-center gap-2"><Atom size={15} className="text-emerald-400/70" /><span className="font-medium tracking-[.12em] text-zinc-400">NÓDULO</span>{institution?.show_institution && institution.institution_name && <span>· {institution.institution_name}{institution.institution_acronym ? ` (${institution.institution_acronym})` : ""}{institution.institution_unit ? ` · ${institution.institution_unit}` : ""}</span>}</div>
          <p>Educação, trabalho e tecnologia de interesse público.</p>
        </div>
      </footer>
    </main>
  );
}
