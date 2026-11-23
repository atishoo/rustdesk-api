import Connection, { ConnectionRuntime, ConnectionTarget } from "./connection";
import { cryptoReady } from "./crypto";
import { controlKeyFromJSON, ControlKey } from "./message";
import { KEY_MAP } from "./gen_js_from_hbb";
import { initZstd } from "./common";
import { loadManagedPassword, readManagedLaunchToken, saveManagedPassword } from "./ljw";
import PCMPlayer from "pcm-player";

export type RemoteEvent = { name: string; [key: string]: unknown };
export type Modifiers = { alt: boolean; ctrl: boolean; shift: boolean; meta: boolean };
export type ManagedRemoteOptions = {
  canvas: HTMLCanvasElement;
  target: ConnectionTarget;
  launchSession: string;
  onEvent(event: RemoteEvent): void;
};

export interface ManagedRemote {
  start(): Promise<void>;
  close(): void;
  login(password: string, remember: boolean): void;
  key(name: string, down: boolean, modifiers: Modifiers, code: string): void;
  mouse(mask: number, x: number, y: number, modifiers: Modifiers): void;
  text(value: string): void;
  releaseInput(): void;
  refresh(): void;
  switchDisplay(index: number): void;
  ctrlAltDel(): void;
}

let dependencies: Promise<void> | undefined;

function loadScript(path: string, available: () => boolean) {
  if (available()) return Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    const timer = window.setTimeout(() => finish(new Error("远程组件加载超时")), 15000);
    const finish = (error?: Error) => {
      window.clearTimeout(timer);
      script.onload = script.onerror = null;
      if (error) { script.remove(); reject(error); }
      else resolve();
    };
    script.src = path;
    script.onload = () => finish(available() ? undefined : new Error("远程组件不可用"));
    script.onerror = () => finish(new Error("远程组件加载失败"));
    document.head.append(script);
  });
}

function ready() {
  dependencies ??= Promise.all([
    cryptoReady,
    initZstd(),
    loadScript("/webclient/yuv-canvas-1.2.6.js", () => Boolean((window as any).YUVCanvas)),
    loadScript("/webclient/ogvjs-1.8.6/ogv.js", () => Boolean((window as any).OGVLoader)),
  ]).then(() => {
    (window as any).OGVLoader.base = "/webclient/ogvjs-1.8.6";
  }).catch((error) => { dependencies = undefined; throw error; });
  return dependencies;
}

const browserKeys: Record<string, string> = {
  Enter: "VK_RETURN", " ": "VK_SPACE", ArrowLeft: "VK_LEFT", ArrowRight: "VK_RIGHT",
  ArrowUp: "VK_UP", ArrowDown: "VK_DOWN", Control: "VK_CONTROL", Shift: "VK_SHIFT",
  Alt: "VK_MENU", Meta: "Meta", OS: "Meta", AltGraph: "RAlt", ContextMenu: "Apps",
  PrintScreen: "VK_SNAPSHOT",
};
const rightKeys: Record<string, string> = { ControlRight: "RControl", ShiftRight: "RShift", AltRight: "RAlt", MetaRight: "RWin" };

export async function createManagedRemote(options: ManagedRemoteOptions): Promise<ManagedRemote> {
  const { canvas, launchSession, onEvent } = options;
  const target = Object.freeze({ ...options.target });
  if (!target.id || !target.server || !/^[a-f0-9]{32}$/.test(launchSession)) throw new Error("远程连接配置无效");
  await ready();
  let closed = false;
  let running = false;
  let attempt = 0;
  let failed = false;
  let display = 0;
  let keyboardAllowed = true;
  let audioWorker: Worker | undefined;
  let player: any;
  let connection: Connection;
  const pressedKeys = new Map<string, string>();
  const pressedButtons = new Set<number>();
  const cursors = new Map<number, string>();
  const sink = (window as any).YUVCanvas.attach(canvas);
  const emit = (name: string, value: Record<string, unknown> = {}) => {
    if (!closed) onEvent({ name, ...value });
  };
  const stopAudio = () => {
    audioWorker?.terminate();
    audioWorker = undefined;
    player?.destroy();
    player = undefined;
  };
  const releaseInput = () => {
    if (connection._ws?._status === "open") {
      for (const name of pressedKeys.values()) connection.inputKey(name, false, false, false, false, false, false);
      for (const button of pressedButtons) connection.inputMouse(button | 2);
    }
    pressedKeys.clear();
    pressedButtons.clear();
  };
  const runtime: ConnectionRuntime = {
    mode: "managed",
    managedTarget: target,
    readManagedToken: () => readManagedLaunchToken(launchSession),
    loadManagedPassword: (value) => loadManagedPassword(value, launchSession),
    saveManagedPassword: (value, password, expected) => saveManagedPassword(value, password, expected, launchSession),
    getServerConf: () => undefined,
    getNetworkOption: () => null,
    getPeers: () => ({}),
    isCurrent: () => !closed,
    isDesktop: () => true,
    msgbox: (type, title, text) => {
      if (type === "error") { failed = true; releaseInput(); connection.close(); stopAudio(); }
      emit("msgbox", { type, title, text });
    },
    draw: (frame) => {
      if (closed) return;
      sink.drawFrame(frame);
      emit("frame", { width: canvas.width, height: canvas.height });
    },
    pushEvent: (name, value) => {
      if (name === "peer_info") display = value.current_display;
      if (name === "switch_display") {
        display = value.display;
        const screen = connection._peerInfo?.displays[display];
        if (screen) Object.assign(screen, { x: value.x, y: value.y, width: value.width, height: value.height });
      }
      if (name === "permission" && typeof value.keyboard === "boolean") {
        if (!value.keyboard) releaseInput();
        keyboardAllowed = value.keyboard;
      }
      if (name === "cursor_data" && value.width > 0 && value.height > 0 && value.colors?.length === value.width * value.height * 4) {
        const image = document.createElement("canvas");
        image.width = value.width;
        image.height = value.height;
        const context = image.getContext("2d");
        if (context) {
          context.putImageData(new ImageData(new Uint8ClampedArray(value.colors), value.width, value.height), 0, 0);
          cursors.set(value.id, `url("${image.toDataURL()}") ${value.hotx} ${value.hoty}, default`);
          canvas.style.cursor = cursors.get(value.id)!;
        }
      } else if (name === "cursor_id") canvas.style.cursor = cursors.get(value.id) || "default";
      emit(name, value);
    },
    copyToClipboard: (text) => emit("clipboard", { text }),
    initAudio: (channels, sampleRate) => {
      stopAudio();
      if (closed) return;
      try {
        player = new PCMPlayer({ channels, sampleRate, inputCodec: "Int16", flushTime: 250 });
        player.volume(1);
        const worker = new Worker("/webclient/libopus.js");
        audioWorker = worker;
        worker.onmessage = (event) => { if (!closed && audioWorker === worker && player) player.feed(event.data); };
        worker.onerror = () => { if (audioWorker === worker) { stopAudio(); emit("audio_error", { text: "远程音频不可用" }); } };
        worker.postMessage({ channels, sampleRate });
      } catch { stopAudio(); emit("audio_error", { text: "远程音频不可用" }); }
    },
    playAudio: (packet) => { if (!closed) audioWorker?.postMessage(packet, [packet.buffer]); },
  };
  connection = new Connection(runtime);
  const canInput = () => !closed && keyboardAllowed && Boolean(connection._peerInfo) && connection._ws?._status === "open";
  const resumeAudio = () => { if (player?.audioCtx?.state === "suspended") void player.continue().catch(() => undefined); };
  const session: ManagedRemote = {
    async start() {
      if (closed) throw new Error("会话已关闭");
      if (running) { releaseInput(); connection.close(); }
      const current = ++attempt;
      stopAudio();
      cursors.clear();
      canvas.style.cursor = "default";
      keyboardAllowed = true;
      failed = false;
      running = true;
      emit("msgbox", { type: "connecting", title: "正在连接", text: "" });
      await connection.start(target.id);
      if (!closed && current === attempt) {
        releaseInput();
        connection.close();
        stopAudio();
        running = false;
        if (!failed) emit("close");
      }
    },
    close() {
      if (closed) return;
      releaseInput();
      closed = true;
      attempt++;
      connection.close();
      stopAudio();
      cursors.clear();
      canvas.style.cursor = "default";
      window.removeEventListener("storage", changedSession);
      const gl = canvas.getContext("webgl");
      gl?.getExtension("WEBGL_lose_context")?.loseContext();
      canvas.width = canvas.height = 0;
    },
    login(password, remember) {
      if (closed || !connection._hash || !password) return;
      connection.setRemember(remember);
      connection.login(password);
    },
    key(value, down, modifiers, code) {
      if (!canInput()) return;
      const physical = code || value;
      const name = pressedKeys.get(physical) || rightKeys[code] || browserKeys[value] || value;
      if (name.length !== 1 && controlKeyFromJSON(KEY_MAP[name] || name) === ControlKey.UNRECOGNIZED) return;
      if (down) pressedKeys.set(physical, name);
      else if (!pressedKeys.delete(physical)) return;
      resumeAudio();
      connection.inputKey(name, down, false, modifiers.alt, modifiers.ctrl, modifiers.shift, modifiers.meta);
    },
    mouse(mask, x, y, modifiers) {
      if (!canInput()) return;
      const kind = mask & 7;
      const button = mask & ~7;
      if (kind === 1) pressedButtons.add(button);
      else if (kind === 2) pressedButtons.delete(button);
      if (kind !== 3) {
        const screen = connection._peerInfo?.displays[display];
        if (screen) {
          x = Math.max(0, Math.min(screen.width - 1, Math.round(x * screen.width / Math.max(1, canvas.width)))) + screen.x;
          y = Math.max(0, Math.min(screen.height - 1, Math.round(y * screen.height / Math.max(1, canvas.height)))) + screen.y;
        }
      }
      resumeAudio();
      if (kind === 1 || kind === 2) {
        connection.inputMouse(0, x, y, modifiers.alt, modifiers.ctrl, modifiers.shift, modifiers.meta);
        x = y = 0;
      }
      connection.inputMouse(mask, x, y, modifiers.alt, modifiers.ctrl, modifiers.shift, modifiers.meta);
    },
    text(value) { if (canInput() && value) connection.inputString(value); },
    releaseInput,
    refresh() { if (!closed) connection.refresh(); },
    switchDisplay(index) {
      if (!closed && connection._peerInfo?.displays[index]) { releaseInput(); connection.switchDisplay(index); }
    },
    ctrlAltDel() { if (canInput()) connection.ctrlAltDel(); },
  };
  const changedSession = (event: StorageEvent) => {
    if (event.storageArea === localStorage && (event.key === "capylink-web-token" || event.key === null) && !readManagedLaunchToken(launchSession)) {
      emit("msgbox", { type: "error", title: "登录状态已变化", text: "请重新登录后再连接。" });
      session.close();
    }
  };
  window.addEventListener("storage", changedSession);
  return session;
}
