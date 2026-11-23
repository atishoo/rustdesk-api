import sodium from "libsodium-wrappers";

export const cryptoReady = sodium.ready;

export async function verify(signed: Uint8Array, key: Uint8Array | string) {
  await cryptoReady;
  const publicKey = typeof key === "string" ? decodeBase64(key) : key;
  return sodium.crypto_sign_open(signed, publicKey);
}

export function decodeBase64(value: string) {
  return sodium.from_base64(value, sodium.base64_variants.ORIGINAL);
}

export function genBoxKeyPair() {
  const pair = sodium.crypto_box_keypair();
  return [pair.privateKey, pair.publicKey];
}

export function genSecretKey() {
  return sodium.crypto_secretbox_keygen();
}

function nonce(value: number) {
  const bytes = new Uint8Array(24);
  for (let index = 0; index < bytes.length && value > 0; index++) {
    const byte = value & 0xff;
    bytes[index] = byte;
    value = (value - byte) / 256;
  }
  return bytes;
}

export function seal(unsigned: Uint8Array, theirKey: Uint8Array, ourKey: Uint8Array) {
  return sodium.crypto_box_easy(unsigned, nonce(0), theirKey, ourKey);
}

export function encrypt(unsigned: Uint8Array, counter: number, key: Uint8Array) {
  return sodium.crypto_secretbox_easy(unsigned, nonce(counter), key);
}

export function decrypt(signed: Uint8Array, counter: number, key: Uint8Array) {
  return sodium.crypto_secretbox_open_easy(signed, nonce(counter), key);
}
