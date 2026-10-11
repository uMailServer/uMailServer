import { useState, useCallback, useRef } from "react";
import type { Account, Domain, QueueEntry } from "@/types";

interface ApiError {
  message: string;
  status?: number;
}

// Errors thrown by apiRequest must be real Error instances: catch sites
// (Login, Domains, Accounts) use "err instanceof Error" to decide whether to
// surface err.message. A plain object makes that check fail and hides the
// server's message behind a generic fallback.
class ApiRequestError extends Error implements ApiError {
  status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "ApiRequestError";
    this.status = status;
  }
}

interface UseApiOptions {
  onError?: (error: ApiError) => void;
  onSuccess?: () => void;
}

const API_BASE = "/api/v1";
export const UNAUTHORIZED_EVENT = "umail:unauthorized";

// The server pages list endpoints (default 100, max 1000). Request the max
// explicitly and keep fetching until a short page, so lists never truncate.
const PAGE_LIMIT = 1000;
const MAX_PAGES = 50;
async function apiRequestAll<T>(endpoint: string): Promise<T[]> {
  const sep = endpoint.includes("?") ? "&" : "?";
  const all: T[] = [];
  for (let page = 0; page < MAX_PAGES; page++) {
    const rows = await apiRequest<T[]>(
      `${endpoint}${sep}limit=${PAGE_LIMIT}&offset=${page * PAGE_LIMIT}`
    );
    if (!Array.isArray(rows)) return rows ?? all;
    all.push(...rows);
    if (rows.length < PAGE_LIMIT) break;
  }
  return all;
}

// Token is now stored in HttpOnly cookie by the server
// No need to read from localStorage (more secure against XSS)
async function apiRequest<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
    credentials: "include", // Send HttpOnly cookies with requests
  });

  if (!response.ok) {
    // Expired/revoked session: let the app drop back to the login screen.
    // Login itself uses its own fetch, so a 401 here always means a stale session.
    if (response.status === 401 && typeof window !== "undefined") {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    }
    const errorData = await response.json().catch(() => ({ error: "Unknown error" }));
    throw new ApiRequestError(errorData.error || "Request failed", response.status);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}

export function useApi<T>() {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const execute = useCallback(
    async (endpoint: string, options?: RequestInit, opts?: UseApiOptions) => {
      setLoading(true);
      setError(null);

      try {
        const result = await apiRequest<T>(endpoint, options);
        setData(result);
        opts?.onSuccess?.();
        return result;
      } catch (err) {
        const apiError = err as ApiError;
        setError(apiError);
        opts?.onError?.(apiError);
        throw err;
      } finally {
        setLoading(false);
      }
    },
    []
  );

  return { data, loading, error, execute, setData };
}

// Domain API hooks
export function useDomains() {
  const [data, setData] = useState<Domain[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const seq = useRef(0);
  const fetchDomains = useCallback(async () => {
    const mine = ++seq.current;
    setLoading(true);
    setError(null);
    try {
      const result = await apiRequestAll<Domain>("/domains");
      if (mine === seq.current) setData(result);
      return result;
    } catch (err) {
      if (mine === seq.current) setError(err as ApiError);
      throw err;
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, []);

  const createDomain = useCallback(async (name: string, maxAccounts?: number) => {
    const result = await apiRequest<Domain>("/domains", {
      method: "POST",
      body: JSON.stringify({ name, max_accounts: maxAccounts }),
    });
    await fetchDomains();
    return result;
  }, [fetchDomains]);

  const updateDomain = useCallback(async (name: string, updates: Partial<Domain>) => {
    const result = await apiRequest<Domain>(`/domains/${encodeURIComponent(name)}`, {
      method: "PUT",
      body: JSON.stringify(updates),
    });
    await fetchDomains();
    return result;
  }, [fetchDomains]);

  const deleteDomain = useCallback(async (name: string) => {
    await apiRequest(`/domains/${encodeURIComponent(name)}`, { method: "DELETE" });
    await fetchDomains();
  }, [fetchDomains]);

  return {
    domains: data,
    loading,
    error,
    fetchDomains,
    createDomain,
    updateDomain,
    deleteDomain,
  };
}

// Account API hooks
export function useAccounts() {
  const [data, setData] = useState<Account[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const seq = useRef(0);
  const fetchAccounts = useCallback(async (domain?: string) => {
    const mine = ++seq.current;
    setLoading(true);
    setError(null);
    try {
      const url = domain ? `/accounts?domain=${encodeURIComponent(domain)}` : "/accounts";
      const result = await apiRequestAll<Account>(url);
      if (mine === seq.current) setData(result);
      return result;
    } catch (err) {
      if (mine === seq.current) setError(err as ApiError);
      throw err;
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, []);

  const createAccount = useCallback(async (email: string, password: string, isAdmin = false) => {
    const result = await apiRequest<Account>("/accounts", {
      method: "POST",
      body: JSON.stringify({ email, password, is_admin: isAdmin }),
    });
    await fetchAccounts();
    return result;
  }, [fetchAccounts]);

  const updateAccount = useCallback(async (email: string, updates: Partial<Account>) => {
    const result = await apiRequest<Account>(`/accounts/${encodeURIComponent(email)}`, {
      method: "PUT",
      body: JSON.stringify(updates),
    });
    await fetchAccounts();
    return result;
  }, [fetchAccounts]);

  const deleteAccount = useCallback(async (email: string) => {
    await apiRequest(`/accounts/${encodeURIComponent(email)}`, { method: "DELETE" });
    await fetchAccounts();
  }, [fetchAccounts]);

  return {
    accounts: data,
    loading,
    error,
    fetchAccounts,
    createAccount,
    updateAccount,
    deleteAccount,
  };
}

// Stats API hook
export function useStats() {
  const [stats, setStats] = useState<{
    domains: number;
    accounts: number;
    messages: number;
    queue_size: number;
  } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const fetchStats = useCallback(async () => {
    setLoading(true);
    try {
      const result = await apiRequest<{
        domains: number;
        accounts: number;
        messages: number;
        queue_size: number;
      }>("/stats");
      setStats(result);
      setError(null);
      return result;
    } catch (err) {
      setError(err as ApiError);
      throw err;
    } finally {
      setLoading(false);
    }
  }, []);

  return { stats, loading, error, fetchStats, setStats };
}

// Queue API hooks
export function useQueue() {
  const [data, setData] = useState<QueueEntry[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const seq = useRef(0);
  const fetchQueue = useCallback(async () => {
    const mine = ++seq.current;
    setLoading(true);
    try {
      const result = await apiRequestAll<QueueEntry>("/queue");
      if (mine === seq.current) {
        setData(result);
        setError(null);
      }
      return result;
    } catch (err) {
      if (mine === seq.current) setError(err as ApiError);
      throw err;
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, []);

  const retryEntry = useCallback(async (id: string) => {
    await apiRequest(`/queue/${encodeURIComponent(id)}`, { method: "POST" });
    await fetchQueue();
  }, [fetchQueue]);

  const dropEntry = useCallback(async (id: string) => {
    await apiRequest(`/queue/${encodeURIComponent(id)}`, { method: "DELETE" });
    await fetchQueue();
  }, [fetchQueue]);

  return {
    entries: data,
    loading,
    error,
    fetchQueue,
    retryEntry,
    dropEntry,
  };
}
