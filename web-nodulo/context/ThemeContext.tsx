"use client";

import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { Moon, Sun } from "lucide-react";

export type AppTheme = "light" | "dark";

type ThemeContextValue = {
  theme: AppTheme;
  toggleTheme: () => void;
};

const THEME_STORAGE_KEY = "nodulo:chat-theme:v1";
const useIsomorphicLayoutEffect = typeof window === "undefined" ? useEffect : useLayoutEffect;
const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<AppTheme>("dark");
  const [ready, setReady] = useState(false);

  useIsomorphicLayoutEffect(() => {
    try {
      const saved = localStorage.getItem(THEME_STORAGE_KEY);
      if (saved === "light" || saved === "dark") {
        document.documentElement.dataset.theme = saved;
        document.documentElement.style.colorScheme = saved;
        setTheme(saved);
      }
    } catch {
      // Mantém o tema escuro padrão se a preferência não estiver disponível.
    }
    setReady(true);
  }, []);

  useIsomorphicLayoutEffect(() => {
    if (!ready) return;
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    try { localStorage.setItem(THEME_STORAGE_KEY, theme); } catch { /* Mantém a preferência durante a sessão. */ }
  }, [ready, theme]);

  const toggleTheme = useCallback(() => setTheme((current) => current === "dark" ? "light" : "dark"), []);
  const value = useMemo(() => ({ theme, toggleTheme }), [theme, toggleTheme]);

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useAppTheme() {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("useAppTheme precisa ser utilizado dentro de ThemeProvider.");
  return value;
}

export function ThemeToggleButton({ className = "" }: { className?: string }) {
  const { theme, toggleTheme } = useAppTheme();
  const nextTheme = theme === "dark" ? "claro" : "escuro";
  return (
    <button
      type="button"
      onClick={toggleTheme}
      className={`global-theme-toggle ${className}`.trim()}
      aria-label={`Ativar tema ${nextTheme}`}
      title={`Ativar tema ${nextTheme}`}
    >
      {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
    </button>
  );
}
