"use client";

import Link from "next/link";
import { Atom, ArrowLeft, Building2, Database, LockKeyhole, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { ThemeToggleButton } from "@/context/ThemeContext";
import { useInstitutionSettings } from "@/hooks/useInstitutionSettings";

export function AuthFrame({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const institution = useInstitutionSettings();
  return (
    <main className="theme-adaptive auth-page relative overflow-hidden bg-[#090c0b] text-zinc-100 selection:bg-emerald-300/25 selection:text-emerald-50">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_15%_22%,rgba(16,90,67,.2),transparent_43%)]" />

      <header className="auth-header relative z-10 flex items-center justify-between border-b border-white/[0.07] px-5 sm:px-8 lg:px-12">
        <Link href="/" className="flex items-center gap-3" aria-label="NÓDULO, página inicial">
          <span className="grid size-9 place-items-center rounded-xl border border-emerald-300/20 bg-emerald-300/8 text-emerald-300"><Atom size={19} strokeWidth={1.7} /></span>
          <span className="text-[14px] font-semibold tracking-[.2em]">NÓDULO</span>
        </Link>
        <div className="flex items-center gap-2">
          <ThemeToggleButton />
          <Link href="/" className="inline-flex items-center gap-2 text-xs text-zinc-500 transition hover:text-zinc-200"><ArrowLeft size={14} /> <span className="hidden sm:inline">Voltar para a página inicial</span><span className="sm:hidden">Início</span></Link>
        </div>
      </header>

      <div className="auth-grid relative z-1 mx-auto grid w-full lg:grid-cols-[1.05fr_.95fr]">
        <section className="auth-left-panel relative hidden flex-col justify-between overflow-hidden border-r border-white/[0.07] pr-8 py-10 lg:flex xl:pr-14 xl:py-12 ">
          <div className="absolute inset-0 bg-[linear-gradient(135deg,rgba(16,90,67,.09),transparent_55%)]" />
          <div className="relative max-w-xl pl-8">  
            <div className="mb-9 inline-flex items-center gap-2 rounded-full border border-emerald-300/15 bg-emerald-300/[0.05] px-3 py-1.5 font-mono text-[9px] tracking-[.17em] text-emerald-200/85"><Sparkles size={12} /> IA ABERTA · DADOS SOB SEU CONTROLE</div>
            <p className="w-full text-[clamp(2.7rem,4.4vw,4.6rem)] font-medium leading-[1.04] tracking-[-.065em] text-zinc-100">Autonomia digital.<br /><span className="text-emerald-300">Sem vender seus dados.</span></p>
            <p className="mt-6 max-w-lg text-[15px] leading-7 text-zinc-400">Um projeto open source para pessoas e organizações que querem usar inteligência artificial sob suas próprias regras.</p>
          </div>

          <div className="relative my-12 flex min-h-67.5 flex-1 items-center justify-center">
            <div className="auth-orbit-ring absolute size-72.5 rounded-full border border-emerald-200/8" />
            <div className="auth-orbit-ring absolute size-52.5 rounded-full border border-emerald-200/10" />
            <div className="auth-orbit-ring absolute size-33 rounded-full border border-emerald-200/12" />
            <div className="auth-orbit-spinner absolute size-72.5 animate-[spin_28s_linear_infinite] rounded-full border border-transparent border-t-emerald-300/35" />
            <div className="auth-orbit-center relative grid size-19 place-items-center rounded-3xl border border-emerald-200/20 bg-[#101915] text-emerald-300 shadow-[0_0_70px_rgba(16,185,129,.13)]"><Atom size={33} strokeWidth={1.25} /></div>
            <div className="auth-orbit-node absolute left-[calc(50%-145px)] top-[calc(50%-20px)] grid size-10 place-items-center rounded-xl border border-white/10 bg-[#111714] text-zinc-300"><Building2 size={17} /></div>
            <div className="auth-orbit-node absolute right-[calc(50%-145px)] top-[calc(50%-20px)] grid size-10 place-items-center rounded-xl border border-white/10 bg-[#111714] text-zinc-300"><Database size={17} /></div>
            <div className="auth-orbit-node auth-orbit-node-accent absolute bottom-[calc(50%-143px)] left-[calc(50%-20px)] grid size-10 place-items-center rounded-xl border border-emerald-200/15 bg-[#111714] text-emerald-300/80"><LockKeyhole size={16} /></div>
          </div>

          {institution?.show_institution && institution.institution_name && <div className="relative border-t border-white/8 pt-5"><p className="font-mono text-[9px] tracking-[.14em] text-zinc-600">INSTITUIÇÃO</p><p className="mt-1 text-xs text-zinc-400">{institution.institution_name}{institution.institution_acronym ? ` · ${institution.institution_acronym}` : ""}</p>{institution.institution_unit && <p className="mt-1 text-[11px] text-zinc-600">{institution.institution_unit}</p>}</div>}
        </section>

        <section className="auth-form-panel flex items-center px-5 py-5 sm:px-10 sm:py-8 lg:px-14 lg:py-8 xl:px-20">
          <div className="auth-form-column w-full max-w-107.5 lg:mx-auto">
            <div className="auth-form-heading mb-6 lg:mb-8">
              <p className="mb-2 font-mono text-[9px] tracking-[.18em] text-emerald-300/80">SOBERANIA DIGITAL · ACESSO PESSOAL</p>
              <h1 className="text-[28px] font-medium tracking-[-.045em] text-zinc-50 sm:text-[32px]">{title}</h1>
              <p className="mt-2 max-w-sm text-sm leading-6 text-zinc-400">{description}</p>
            </div>
            {children}
            <div className="auth-session-note mt-5 flex items-center gap-2 border-t border-white/[0.07] pt-4 font-mono text-[9px] tracking-[.08em] text-zinc-600"><span className="size-1.5 rounded-full bg-emerald-400/80" /> SEUS DADOS NÃO SÃO PRODUTO</div>
          </div>
        </section>
      </div>
    </main>
  );
}
