// Browser side of WebAuthn: the server sends options as JSON with base64url binary fields, the browser
// API wants ArrayBuffers; the answer goes back as JSON again.

type Json = Record<string, unknown>;

function b64urlToBuf(value: string): ArrayBuffer {
  const pad = '='.repeat((4 - (value.length % 4)) % 4);
  const bin = atob((value + pad).replace(/-/g, '+').replace(/_/g, '/'));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function bufToB64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Passkeys need a secure origin (HTTPS or localhost) and a browser with the API. */
export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext && typeof window.PublicKeyCredential === 'function';
}

function descriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) return undefined;
  return list.map((d) => ({ ...(d as Json), id: b64urlToBuf((d as Json).id as string) }) as PublicKeyCredentialDescriptor);
}

/** Asks the authenticator to create a passkey; returns the JSON the server expects. */
export async function createPasskey(publicKey: Json): Promise<unknown> {
  const user = publicKey.user as Json;
  const opts = {
    ...publicKey,
    challenge: b64urlToBuf(publicKey.challenge as string),
    user: { ...user, id: b64urlToBuf(user.id as string) },
    excludeCredentials: descriptors(publicKey.excludeCredentials),
  } as unknown as PublicKeyCredentialCreationOptions;
  const cred = (await navigator.credentials.create({ publicKey: opts })) as PublicKeyCredential | null;
  if (!cred) throw new Error('no credential');
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: bufToB64url(r.clientDataJSON),
      attestationObject: bufToB64url(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : undefined,
    },
    clientExtensionResults: cred.getClientExtensionResults(),
  };
}

/** Asks for a passkey to sign in (the browser shows the list of passkeys for this site). */
export async function getPasskey(publicKey: Json): Promise<unknown> {
  const opts = {
    ...publicKey,
    challenge: b64urlToBuf(publicKey.challenge as string),
    allowCredentials: descriptors(publicKey.allowCredentials),
  } as unknown as PublicKeyCredentialRequestOptions;
  const cred = (await navigator.credentials.get({ publicKey: opts })) as PublicKeyCredential | null;
  if (!cred) throw new Error('no credential');
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: bufToB64url(r.clientDataJSON),
      authenticatorData: bufToB64url(r.authenticatorData),
      signature: bufToB64url(r.signature),
      userHandle: r.userHandle ? bufToB64url(r.userHandle) : undefined,
    },
    clientExtensionResults: cred.getClientExtensionResults(),
  };
}

/** True when the user closed or refused the browser dialog (nothing to report). */
export function passkeyCancelled(e: unknown): boolean {
  return e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError');
}
