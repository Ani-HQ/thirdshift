const API_BASE = process.env.NEXT_PUBLIC_THIRDSHIFT_API_BASE || "";

export async function apiFetch<T>(path: string, token: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      ...(init.headers || {})
    }
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || response.statusText);
  }
  return (await response.json()) as T;
}

export async function apiAction(path: string, token: string, reason = ""): Promise<void> {
  await apiFetch(path, token, {
    method: "POST",
    body: JSON.stringify({ reason })
  });
}

export function nodeActionPath(nodeID: string, action: "drain" | "pause" | "quarantine") {
  return `/internal/v1/nodes/${encodeURIComponent(nodeID)}/${action}`;
}

export function jobActionPath(jobID: string, action: "retry" | "cancel") {
  return `/internal/v1/jobs/${encodeURIComponent(jobID)}/${action}`;
}
