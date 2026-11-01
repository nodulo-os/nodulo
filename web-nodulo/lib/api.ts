const configuredApiURL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8090";

export const API_BASE_URL = configuredApiURL
  .replace(/\/api\/chat\/?$/, "")
  .replace(/\/api\/?$/, "")
  .replace(/\/+$/, "");

export function apiURL(path: string): string {
  return `${API_BASE_URL}${path.startsWith("/") ? path : `/${path}`}`;
}

export function bearerHeaders(token: string | null, headers?: HeadersInit): Headers {
  const result = new Headers(headers);
  if (token) result.set("Authorization", `Bearer ${token}`);
  return result;
}
