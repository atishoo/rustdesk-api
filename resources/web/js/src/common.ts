import * as zstd from "zstddec";
import { KeyEvent, controlKeyFromJSON, ControlKey } from "./message";
import { KEY_MAP, LANGS } from "./gen_js_from_hbb";

let decompressor: zstd.ZSTDDecoder;

export async function initZstd() {
  const tmp = new zstd.ZSTDDecoder();
  await tmp.init();
  console.log("zstd ready");
  decompressor = tmp;
}

export async function decompress(compressedArray: Uint8Array) {
  const MAX = 1024 * 1024 * 64;
  const MIN = 1024 * 1024;
  let n = 30 * compressedArray.length;
  if (n > MAX) {
    n = MAX;
  }
  if (n < MIN) {
    n = MIN;
  }
  try {
    if (!decompressor) {
      await initZstd();
    }
    return decompressor.decode(compressedArray, n);
  } catch (e) {
    console.error("decompress failed: " + e);
    return undefined;
  }
}

const LANG = getLang();

export function translate(locale: string, text: string): string {
  const lang = LANG || locale.substring(locale.length - 2).toLowerCase();
  let en = LANGS.en as any;
  let dict = (LANGS as any)[lang];
  if (!dict) dict = en;
  let res = dict[text];
  if (!res && lang != "en") res = en[text];
  return res || text;
}

const zCode = "z".charCodeAt(0);
const aCode = "a".charCodeAt(0);

export function mapKey(name: string, isDesktop: Boolean) {
  const tmp = KEY_MAP[name] || name;
  if (tmp.length == 1) {
    const chr = tmp.charCodeAt(0);
    if (!isDesktop && (chr > zCode || chr < aCode))
      return KeyEvent.fromPartial({ unicode: chr });
    else return KeyEvent.fromPartial({ chr });
  }
  const control_key = controlKeyFromJSON(tmp);
  if (control_key == ControlKey.UNRECOGNIZED) {
    console.error("Unknown control key " + tmp);
  }
  return KeyEvent.fromPartial({ control_key });
}

export async function sleep(ms: number) {
  await new Promise((r) => setTimeout(r, ms));
}

function getLang(): string {
  try {
    const queryString = window.location.search;
    const urlParams = new URLSearchParams(queryString);
    return urlParams.get("lang") || "";
  } catch (e) {
    return "";
  }
}

export const PORT = 21116;
const SCHEMA = location.protocol === "https:" ? "wss://" : "ws://";
export const websocketScheme = /^wss?:\/\//i;
export const mixedContentWebSocketError = new Error("HTTPS 页面需要使用 WSS 服务端");
export const proxyIdPath = /\/ws\/id(\/?)$/;

export function matchesRendezvous(uri: URL | string, server: unknown): boolean {
  if (!uri || typeof server !== "string" || !server) return false;
  try {
    const actual = typeof uri === "string" ? new URL(getWebClientUri(uri, false)) : uri;
    return actual.href === new URL(getWebClientUri(server, false)).href;
  } catch {
    return false;
  }
}

export function getWebClientUri(server: string, isRelay: boolean, relayEndpoint = false): string {
  const websocket = websocketScheme.test(server);
  const ipv6 = !websocket && !server.startsWith("[") && server.indexOf(":") !== server.lastIndexOf(":");
  let address: URL;
  try {
    address = new URL(websocket ? server : `rustdesk-id://${ipv6 ? `[${server}]` : server}`);
  } catch {
    throw new Error("服务端地址无效");
  }
  if (!address.hostname || address.username || address.password || address.port === "0" || /[?#\\\s\x00-\x1f\x7f-\x9f]/.test(server)) {
    throw new Error("服务端地址无效");
  }
  if (websocket) {
    const hostname = address.hostname.replace(/\.$/, "");
    const loopback = hostname === "localhost" || hostname.endsWith(".localhost") || hostname === "[::1]" || /^127(?:\.\d+){3}$/.test(hostname);
    if (location.protocol === "https:" && address.protocol === "ws:" && !loopback) {
      throw mixedContentWebSocketError;
    }
    if (!isRelay || relayEndpoint) return server;
    if (!proxyIdPath.test(address.pathname)) throw new Error("缺少 Relay WebSocket 地址");
    address.pathname = address.pathname.replace(proxyIdPath, "/ws/relay$1");
    return address.href;
  }
  const basePort = Number(address.port || (isRelay ? PORT + 1 : PORT));
  const port = basePort + (isRelay && !relayEndpoint && address.port ? 3 : 2);
  if (address.pathname || !basePort || port > 65535) throw new Error("服务端地址无效");
  return new URL(`${SCHEMA}${address.hostname}:${port}`).href;
}
