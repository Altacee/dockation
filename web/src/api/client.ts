import type {
  APIResponse,
  Container,
  Image,
  Volume,
  Network,
  ComposeStack,
  Peer,
  PairingCode,
  MigrationState,
  MigrationOptions,
  DryRunResult,
  ResourceCounts,
  SelectedResource,
} from '../types';

const API_BASE = import.meta.env.VITE_API_BASE || '/api';

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<APIResponse<T>> {
  try {
    const response = await fetch(`${API_BASE}${url}`, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    });

    const data = await response.json();

    if (!response.ok) {
      return {
        success: false,
        error: data.error || `HTTP ${response.status}: ${response.statusText}`,
      };
    }

    return {
      success: true,
      data: data.data || data,
    };
  } catch (error) {
    return {
      success: false,
      error: error instanceof Error ? error.message : 'Network error',
    };
  }
}

// Resource APIs
export const api = {
  // Containers
  containers: {
    list: () => fetchJSON<Container[]>('/containers?all=true'),
    get: (id: string) => fetchJSON<Container>(`/containers/${id}`),
    start: (id: string) => fetchJSON<void>(`/containers/${id}/start`, { method: 'POST' }),
    stop: (id: string) => fetchJSON<void>(`/containers/${id}/stop`, { method: 'POST' }),
    restart: (id: string) => fetchJSON<void>(`/containers/${id}/restart`, { method: 'POST' }),
    remove: (id: string, force?: boolean) =>
      fetchJSON<void>(`/containers/${id}${force ? '?force=true' : ''}`, { method: 'DELETE' }),
    logs: (id: string, tail?: string) =>
      fetch(`${API_BASE}/containers/${id}/logs?tail=${tail || '100'}`).then((r) => r.text()),
    logsStream: (id: string) => `${API_BASE}/containers/${id}/logs?follow=true`,
  },

  // Images
  images: {
    list: () => fetchJSON<Image[]>('/images'),
    get: (id: string) => fetchJSON<Image>(`/images/${id}`),
    pull: (image: string) =>
      fetchJSON<void>('/images/pull', {
        method: 'POST',
        body: JSON.stringify({ image }),
      }),
    remove: (id: string, force?: boolean) =>
      fetchJSON<void>(`/images/${id}${force ? '?force=true' : ''}`, { method: 'DELETE' }),
  },

  // Volumes
  volumes: {
    list: () => fetchJSON<Volume[]>('/volumes'),
    get: (name: string) => fetchJSON<Volume>(`/volumes/${name}`),
    create: (name: string, labels?: Record<string, string>) =>
      fetchJSON<void>('/volumes', {
        method: 'POST',
        body: JSON.stringify({ name, labels }),
      }),
    remove: (name: string, force?: boolean) =>
      fetchJSON<void>(`/volumes/${name}${force ? '?force=true' : ''}`, { method: 'DELETE' }),
  },

  // Networks
  networks: {
    list: () => fetchJSON<Network[]>('/networks'),
    get: (id: string) => fetchJSON<Network>(`/networks/${id}`),
    create: (name: string, driver?: string, options?: { internal?: boolean; attachable?: boolean }) =>
      fetchJSON<void>('/networks', {
        method: 'POST',
        body: JSON.stringify({ name, driver: driver || 'bridge', ...options }),
      }),
    remove: (id: string) => fetchJSON<void>(`/networks/${id}`, { method: 'DELETE' }),
  },

  // Compose Stacks
  compose: {
    list: () => fetchJSON<ComposeStack[]>('/compose'),
    get: (name: string) => fetchJSON<ComposeStack>(`/compose/${name}`),
  },

  // Resource Counts
  resources: {
    counts: () => fetchJSON<ResourceCounts>('/resources/counts'),
  },

  // Peers
  peers: {
    list: () => fetchJSON<Peer[]>('/peers'),
    get: (id: string) => fetchJSON<Peer>(`/peers/${id}`),
    disconnect: (id: string) => fetchJSON<void>(`/peers/${id}/disconnect`, { method: 'POST' }),
  },

  // Pairing
  pairing: {
    generate: () => fetchJSON<PairingCode>('/pair/generate', { method: 'POST' }),
    connect: (code: string) =>
      fetchJSON<Peer>('/pair/connect', {
        method: 'POST',
        body: JSON.stringify({ code }),
      }),
    cancel: (code: string) =>
      fetchJSON<void>('/pair/cancel', {
        method: 'POST',
        body: JSON.stringify({ code }),
      }),
  },

  // Migration
  migration: {
    create: (targetPeerId: string, resources: SelectedResource[], options: MigrationOptions) =>
      fetchJSON<MigrationState>('/migrate', {
        method: 'POST',
        body: JSON.stringify({ targetPeerId, resources, options }),
      }),

    dryRun: (targetPeerId: string, resources: SelectedResource[], options: MigrationOptions) =>
      fetchJSON<DryRunResult>('/migrate/dry-run', {
        method: 'POST',
        body: JSON.stringify({ targetPeerId, resources, options }),
      }),

    get: (id: string) => fetchJSON<MigrationState>(`/migrate/${id}`),

    pause: (id: string) =>
      fetchJSON<void>(`/migrate/${id}/pause`, { method: 'POST' }),

    resume: (id: string) =>
      fetchJSON<void>(`/migrate/${id}/resume`, { method: 'POST' }),

    cancel: (id: string) =>
      fetchJSON<void>(`/migrate/${id}/cancel`, { method: 'POST' }),

    retry: (id: string) =>
      fetchJSON<void>(`/migrate/${id}/retry`, { method: 'POST' }),

    list: () => fetchJSON<MigrationState[]>('/migrate'),
  },
};

export default api;
