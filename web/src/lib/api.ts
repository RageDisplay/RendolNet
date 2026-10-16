export class ApiError extends Error {
  status: number;
  body: any;
  constructor(status: number, message: string, body: any) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(method: string, path: string, body?: unknown, raw?: BodyInit): Promise<T> {
  const headers: Record<string, string> = { "X-RN-Request": "1" };
  let payload: BodyInit | undefined = raw;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const resp = await fetch(path, { method, headers, body: payload, credentials: "same-origin" });
  if (resp.status === 204) return undefined as T;
  const text = await resp.text();
  let data: any = undefined;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!resp.ok) {
    if (resp.status === 401 && !path.startsWith("/v1/auth/login")) onUnauthorized();
    const msg =
      (data && typeof data === "object" && (data.error || (data.errors && data.errors[0]?.message))) ||
      resp.statusText ||
      `HTTP ${resp.status}`;
    throw new ApiError(resp.status, msg, data);
  }
  return data as T;
}

export const api = {
  get: <T>(p: string) => request<T>("GET", p),
  post: <T>(p: string, b?: unknown) => request<T>("POST", p, b ?? {}),
  put: <T>(p: string, b: unknown) => request<T>("PUT", p, b),
  del: <T>(p: string) => request<T>("DELETE", p),
  upload: <T>(p: string, file: Blob) => request<T>("POST", p, undefined, file),
};
