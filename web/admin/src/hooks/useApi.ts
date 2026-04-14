import { useState, useCallback } from "react";
import type { Account, Domain, QueueEntry } from "@/types";

interface ApiError {
  message: string;
  status?: number;
}

interface UseApiOptions {
  onError?: (error: ApiError) => void;
  onSuccess?: () => void;
}

const API_BASE = "/api/v1";

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
    const errorData = await response.json().catch(() => ({ error: "Unknown error" }));
    throw { message: errorData.error || "Request failed", status: response.status };
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

  const fetchDomains = useCallback(async () => {
    setLoading(true);
    try {
      const result = await apiRequest<Domain[]>("/domains");
      setData(result);
      return result;
    } catch (err) {
      setError(err as ApiError);
      throw err;
    } finally {
      setLoading(false);
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
    const result = await apiRequest<Domain>(`/domains/${name}`, {
      method: "PUT",
      body: JSON.stringify(updates),
    });
    await fetchDomains();
    return result;
  }, [fetchDomains]);

  const deleteDomain = useCallback(async (name: string) => {
    await apiRequest(`/domains/${name}`, { method: "DELETE" });
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

  const fetchAccounts = useCallback(async (domain?: string) => {
    setLoading(true);
    try {
      const url = domain ? `/accounts?domain=${domain}` : "/accounts";
      const result = await apiRequest<Account[]>(url);
      setData(result);
      return result;
    } catch (err) {
      setError(err as ApiError);
      throw err;
    } finally {
      setLoading(false);
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
    const result = await apiRequest<Account>(`/accounts/${email}`, {
      method: "PUT",
      body: JSON.stringify(updates),
    });
    await fetchAccounts();
    return result;
  }, [fetchAccounts]);

  const deleteAccount = useCallback(async (email: string) => {
    await apiRequest(`/accounts/${email}`, { method: "DELETE" });
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
      return result;
    } finally {
      setLoading(false);
    }
  }, []);

  return { stats, loading, fetchStats, setStats };
}

// Queue API hooks
export function useQueue() {
  const [data, setData] = useState<QueueEntry[] | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchQueue = useCallback(async () => {
    setLoading(true);
    try {
      const result = await apiRequest<QueueEntry[]>("/queue");
      setData(result);
      return result;
    } finally {
      setLoading(false);
    }
  }, []);

  const retryEntry = useCallback(async (id: string) => {
    await apiRequest(`/queue/${id}`, { method: "POST" });
    await fetchQueue();
  }, [fetchQueue]);

  const dropEntry = useCallback(async (id: string) => {
    await apiRequest(`/queue/${id}`, { method: "DELETE" });
    await fetchQueue();
  }, [fetchQueue]);

  return {
    entries: data,
    loading,
    fetchQueue,
    retryEntry,
    dropEntry,
  };
}
