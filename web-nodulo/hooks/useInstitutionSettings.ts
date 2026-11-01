"use client";

import { useEffect, useState } from "react";
import { apiURL } from "@/lib/api";

export type InstitutionalSettings = {
  show_institution: boolean;
  institution_name: string;
  institution_acronym: string;
  institution_unit: string;
  updated_at?: string;
};

export function useInstitutionSettings() {
  const [settings, setSettings] = useState<InstitutionalSettings | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    fetch(apiURL("/api/site-settings"), { signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<InstitutionalSettings> : null)
      .then((result) => { if (result) setSettings(result); })
      .catch(() => { /* Identidade institucional é opcional; sem API, ocultamos os dados. */ });
    return () => controller.abort();
  }, []);

  return settings;
}
