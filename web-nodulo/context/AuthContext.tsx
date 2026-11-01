"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useRouter } from "next/navigation";
import { apiURL, bearerHeaders } from "@/lib/api";
import { apiErrorMessage, userFacingError } from "@/lib/errors";

export type AuthUser = {
  id: string;
  name: string;
  email: string;
  role: "admin" | "user";
};

type AuthResponse = { user: AuthUser; token: string };
type RegisterResult = { status: "approved" | "pending"; message: string };

type AuthContextValue = {
  user: AuthUser | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<RegisterResult>;
  logout: () => void;
  authorizedFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  homeForUser: (user?: AuthUser | null) => "/admin" | "/chat";
};

const TOKEN_KEY = "nodulo:token";
const USER_KEY = "nodulo:user";
const COOKIE_KEY = "nodulo_token";
const AuthContext = createContext<AuthContextValue | null>(null);

function readCookie(name: string): string | null {
  if (typeof document === "undefined") return null;
  const prefix = `${name}=`;
  const entry = document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith(prefix));
  return entry ? decodeURIComponent(entry.slice(prefix.length)) : null;
}

function persistSession(user: AuthUser, token: string) {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(USER_KEY, JSON.stringify(user));
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  document.cookie = `${COOKIE_KEY}=${encodeURIComponent(token)}; Path=/; Max-Age=86400; SameSite=Lax${secure}`;
}

function clearSession() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  document.cookie = `${COOKIE_KEY}=; Path=/; Max-Age=0; SameSite=Lax`;
}

async function responseError(response: Response): Promise<Error> {
  return new Error(await apiErrorMessage(response, "Não foi possível concluir a solicitação de autenticação. Tente novamente."));
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [user, setUser] = useState<AuthUser | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const sessionCheckController = useRef<AbortController | null>(null);

  const homeForUser = useCallback((candidate?: AuthUser | null) =>
    candidate?.role === "admin" ? "/admin" : "/chat", []);

  const authorizedFetch = useCallback((input: RequestInfo | URL, init: RequestInit = {}) => {
    return fetch(input, {
      ...init,
      headers: bearerHeaders(token, init.headers),
    });
  }, [token]);

  const acceptSession = useCallback((session: AuthResponse) => {
    persistSession(session.user, session.token);
    setUser(session.user);
    setToken(session.token);
    router.replace(homeForUser(session.user));
  }, [homeForUser, router]);

  useEffect(() => {
    let active = true;
    const savedToken = localStorage.getItem(TOKEN_KEY) || readCookie(COOKIE_KEY);
    let cachedUser: AuthUser | null = null;
    try {
      const parsed: unknown = JSON.parse(localStorage.getItem(USER_KEY) || "null");
      if (parsed && typeof parsed === "object" && "id" in parsed && "role" in parsed) {
        cachedUser = parsed as AuthUser;
      }
    } catch {
      localStorage.removeItem(USER_KEY);
    }

    if (!savedToken) {
      setIsLoading(false);
      return () => { active = false; };
    }

    const controller = new AbortController();
    sessionCheckController.current = controller;
    setToken(savedToken);
    setUser(cachedUser);
    fetch(apiURL("/api/auth/me"), {
      headers: bearerHeaders(savedToken),
      signal: controller.signal,
    })
      .then(async (response) => {
        if (response.status === 401) {
          clearSession();
          if (active) {
            setToken(null);
            setUser(null);
          }
          return;
        }
        if (!response.ok) throw await responseError(response);
        const profile = await response.json() as AuthUser;
        if (!active) return;
        setUser(profile);
        localStorage.setItem(USER_KEY, JSON.stringify(profile));
      })
      .catch(() => {
        // Mantém o perfil em cache quando a API está temporariamente indisponível.
      })
      .finally(() => {
        if (sessionCheckController.current === controller) {
          sessionCheckController.current = null;
        }
        if (active) setIsLoading(false);
      });

    return () => {
      active = false;
      controller.abort();
    };
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    let response: Response;
    try {
      response = await fetch(apiURL("/api/auth/login"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
    } catch (cause) {
      throw new Error(userFacingError(cause, "Não foi possível conectar à API do NÓDULO. Verifique sua conexão e tente novamente."));
    }
    if (!response.ok) throw await responseError(response);
    acceptSession(await response.json() as AuthResponse);
  }, [acceptSession]);

  const register = useCallback(async (name: string, email: string, password: string) => {
    let response: Response;
    try {
      response = await fetch(apiURL("/api/auth/register"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, email, password }),
      });
    } catch (cause) {
      throw new Error(userFacingError(cause, "Não foi possível conectar à API do NÓDULO. Verifique sua conexão e tente novamente."));
    }
    if (!response.ok) throw await responseError(response);
    const result = await response.json() as {
      status?: string;
      message?: string;
      user?: AuthUser;
      token?: string;
    };
    if (result.status === "pending") {
      return {
        status: "pending" as const,
        message: result.message || "Seu cadastro está em análise. Você receberá um e-mail quando a administração tomar uma decisão.",
      };
    }
    if (!result.user || !result.token) throw new Error("A API não confirmou a autorização do cadastro.");
    acceptSession({ user: result.user, token: result.token });
    return {
      status: "approved" as const,
      message: "E-mail autorizado. Seu acesso foi liberado.",
    };
  }, [acceptSession]);

  const logout = useCallback(() => {
    sessionCheckController.current?.abort();
    sessionCheckController.current = null;
    clearSession();
    setUser(null);
    setToken(null);
    setIsLoading(false);
    // ProtectedPage redirects unauthenticated users to /login. Use the same
    // destination here to avoid racing its guard with a separate root redirect.
    router.replace("/login");
  }, [router]);

  const value = useMemo<AuthContextValue>(() => ({
    user,
    token,
    isAuthenticated: Boolean(token && user),
    isLoading,
    login,
    register,
    logout,
    authorizedFetch,
    homeForUser,
  }), [user, token, isLoading, login, register, logout, authorizedFetch, homeForUser]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error("useAuth precisa ser usado dentro de AuthProvider.");
  return context;
}
