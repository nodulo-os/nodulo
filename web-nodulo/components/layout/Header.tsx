"use client";

import { LogOut, Menu, Moon, ShieldCheck, Sun } from "lucide-react";
import Link from "next/link";
import { useAuth } from "@/context/AuthContext";

interface HeaderProps {
  theme: "light" | "dark";
  onToggleTheme: () => void;
  onOpenMobileSidebar: () => void;
}

export function Header({ theme, onToggleTheme, onOpenMobileSidebar }: HeaderProps) {
  const { user, logout } = useAuth();
  return (
    <header className="app-header flex h-14 shrink-0 items-center px-4 sm:px-6">
      <button
        type="button"
        onClick={onOpenMobileSidebar}
        className="icon-button mobile-sidebar-trigger"
        aria-label="Abrir barra lateral"
        title="Abrir barra lateral"
      >
        <Menu size={19} />
      </button>
      <div className="header-account-group ml-auto flex shrink-0 items-center gap-2">
        <button
          type="button"
          onClick={onToggleTheme}
          className="icon-button theme-toggle"
          aria-label={`Ativar tema ${theme === "dark" ? "claro" : "escuro"}`}
          title={`Ativar tema ${theme === "dark" ? "claro" : "escuro"}`}
        >
          {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
        </button>
        {user?.role === "admin" && (
          <Link
            href="/admin"
            className="header-admin-link"
            aria-label="Abrir painel de administração"
            title="Painel de administração"
          >
            <ShieldCheck size={15} aria-hidden="true" />
            <span className="header-admin-label">Admin</span>
          </Link>
        )}
        <Link href="/profile" className="profile-account profile-account-link" aria-label="Abrir perfil do usuário" title="Perfil do usuário">
          <span>{user?.name ?? "Conta"}</span>
          <div className="profile-chip" aria-hidden="true">{user?.name.trim().charAt(0).toUpperCase() || "N"}</div>
        </Link>
        <button type="button" onClick={logout} className="icon-button" aria-label="Sair da conta" title="Sair"><LogOut size={16} /></button>
      </div>
    </header>
  );
}
