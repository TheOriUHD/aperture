import { create, get, supported } from "@github/webauthn-json";
import { api } from "./api";
import type { User } from "./types";

// passkeysSupported reports whether this browser/context can use WebAuthn.
// Requires a secure context (HTTPS or localhost).
export function passkeysSupported(): boolean {
  try {
    return supported();
  } catch {
    return false;
  }
}

// registerPasskey enrolls a new passkey for the signed-in user.
export async function registerPasskey(name: string): Promise<void> {
  const options = await api.post<{ publicKey: unknown }>("/api/auth/passkey/register/begin");
  // create() turns the server's base64url options into a real credential and
  // back into JSON the server can verify.
  const credential = await create(options as Parameters<typeof create>[0]);
  await api.post(`/api/auth/passkey/register/finish?name=${encodeURIComponent(name)}`, credential);
}

// loginPasskey performs a passwordless (discoverable) sign-in.
export async function loginPasskey(): Promise<User> {
  const options = await api.post<{ publicKey: unknown }>("/api/auth/passkey/login/begin");
  const assertion = await get(options as Parameters<typeof get>[0]);
  return api.post<User>("/api/auth/passkey/login/finish", assertion);
}

// passkeyError maps WebAuthn DOMExceptions to friendly copy.
export function passkeyError(e: unknown): string {
  const name = (e as { name?: string })?.name;
  if (name === "NotAllowedError") return "Cancelled or timed out — try again.";
  if (name === "InvalidStateError") return "This device already has a passkey for your account.";
  return (e as Error)?.message || "Something went wrong with the passkey.";
}
