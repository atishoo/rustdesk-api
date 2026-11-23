import Websock from "./websock";
import * as message from "./message.js";
import * as rendezvous from "./rendezvous.js";
import { loadVp9 } from "./codec";
import { decodeSharedPassword } from "./ljw";
import * as sha256 from "fast-sha256";
import * as crypto from "./crypto";
import { decompress, getWebClientUri, mapKey, matchesRendezvous, mixedContentWebSocketError, PORT, proxyIdPath, sleep, websocketScheme } from "./common";

//根据协议设置为ws或wss
const managedSessionChanged = new Error("登录状态无法验证，请重新登录后再连接。");
const sharedPasswordRejected = "设备拒绝了分享密码，请联系分享者生成新的分享链接。";

type MsgboxCallback = (type: string, title: string, text: string) => void;
type DrawCallback = (data: any) => void;
type RelayTarget = { server: string; relay: string };
export type ConnectionTarget = RelayTarget & { id: string; key: string };
type SharedTarget = ConnectionTarget & { passwordBase64: string | undefined; options: { info: object } };

export interface ConnectionRuntime {
  mode: "managed" | "shared" | "ordinary";
  managedTarget?: ConnectionTarget;
  sharedTarget?: SharedTarget;
  readManagedToken(): string | undefined;
  loadManagedPassword(target: ConnectionTarget): { password?: string; remember?: boolean };
  saveManagedPassword(target: ConnectionTarget, password?: string, expected?: string): void;
  getServerConf(token: string): Promise<any> | undefined;
  getNetworkOption(name: string): string | null;
  getPeers(): Record<string, any>;
  isCurrent(connection: Connection): boolean;
  msgbox: MsgboxCallback;
  draw: DrawCallback;
  pushEvent(name: string, payload: any): void;
  copyToClipboard(value: string): void;
  initAudio(channels: number, sampleRate: number): void;
  playAudio(packet: Uint8Array): void;
  isDesktop(): boolean;
}

declare global {
  interface Window {
    capylinkManagedTarget?: ConnectionTarget;
    capylinkWebClientApiServer?: string;
    capylinkWebClientRelayTarget?: RelayTarget;
  }
}

//const cursorCanvas = document.createElement("canvas");

export default class Connection {
  _msgs: any[];
  _ws: Websock | undefined;
  _interval: any;
  _id: string;
  _hash: message.Hash | undefined;
  _msgbox: MsgboxCallback;
  _draw: DrawCallback;
  _peerInfo: message.PeerInfo | undefined;
  _firstFrame: Boolean | undefined;
  _videoDecoder: any;
  _videoGeneration = 0;
  _videoLoad: AbortController | undefined;
  _password: Uint8Array | undefined;
  _managedTarget: ConnectionTarget | undefined;
  _managedSessionCheck: AbortController | undefined;
  _managedToken: string | undefined;
  _managedPasswordAttempt: string | undefined;
  _ordinaryAccountToken: string | null = null;
  _ordinaryNetwork?: { server: string | null; key: string | null };
  _startGeneration = 0;
  _sharedTarget: SharedTarget | undefined;
  _sharePasswordTried = false;
  _options: any;
  //_cursors: { [name: number]: any };

  constructor(private runtime: ConnectionRuntime) {
    this._msgbox = runtime.msgbox;
    this._draw = runtime.draw;
    this._msgs = [];
    this._id = "";
    if (runtime.mode === "managed") this._options = {};
    //this._cursors = {};
  }

  async start(id: string) {
    const generation = this._startGeneration + 1;
    try {
      await this._start(id);
    } catch (e: any) {
      if (generation !== this._startGeneration) return;
      if (e === managedSessionChanged) {
        this.msgbox("error", "Login Error", managedSessionChanged.message);
        return;
      }
      this.msgbox(
        "error",
        "Connection Error",
        e?.type == "close" ? "Reset by the peer" : String(e)
      );
    }
  }

  async _start(id: string) {
    const generation = ++this._startGeneration;
    this._peerInfo = undefined;
    this._firstFrame = false;
    this._hash = undefined;
    const managedTarget = this.runtime.managedTarget;
    const sharedTarget = this.runtime.sharedTarget;
    if (this.runtime.mode === "managed" && (!id || !managedTarget || managedTarget.id !== id)) throw new Error("受管远程配置不可用");
    if (this.runtime.mode === "shared") {
      if (!sharedTarget || sharedTarget.id !== id) throw new Error("分享链接不可用或连接目标无效");
      if (sharedTarget.passwordBase64 === undefined) {
        this.msgbox("error", "Login Error", sharedPasswordRejected);
        return;
      }
    }
    this._managedTarget = this.runtime.mode === "managed" ? managedTarget : undefined;
    this._sharedTarget = this.runtime.mode === "shared" ? sharedTarget : undefined;
    if (this._sharedTarget) {
      this._password = undefined;
      this._sharePasswordTried = false;
    }
    const target = this._managedTarget ?? this._sharedTarget;
    const requestToken = this._managedTarget ? this.runtime.readManagedToken() : undefined;
    if (this._managedTarget) {
      if (!requestToken) throw managedSessionChanged;
      this._managedSessionCheck?.abort();
      const check = new AbortController();
      this._managedSessionCheck = check;
      const timeout = window.setTimeout(() => check.abort(), 5000);
      let deployment: { id_server: string; ws_host?: string; key: string; relay_server: string } | undefined;
      try {
        const response = await fetch("/api/admin/config/server", {
          headers: { "api-token": requestToken },
          mode: "same-origin",
          credentials: "omit",
          redirect: "error",
          cache: "no-store",
          signal: check.signal,
        });
        if (response.ok) {
          const result = await response.json();
          if (result?.code === 0) deployment = result.data;
        }
      } catch {
        // A failed request cannot confirm the console session.
      } finally {
        window.clearTimeout(timeout);
      }
      if (this._managedSessionCheck !== check) return;
      this._managedSessionCheck = undefined;
      if (!deployment || this.runtime.readManagedToken() !== requestToken) throw managedSessionChanged;
      if ((deployment.ws_host || deployment.id_server) !== this._managedTarget.server || deployment.key !== this._managedTarget.key ||
          deployment.relay_server !== this._managedTarget.relay) {
        throw new Error("服务配置已变化，请关闭该会话后重新选择设备。");
      }
      this._managedToken = requestToken;
      const { password, remember } = this.runtime.loadManagedPassword(this._managedTarget);
      this._options.password = password;
      this._options.remember = remember;
      this._password = undefined;
      this._hash = undefined;
      this._managedPasswordAttempt = undefined;
    }
    const accountToken = this._managedTarget ? null : sessionStorage.getItem("access_token");
    if (!this._managedTarget && !accountToken) {
      this.msgbox("error", "Login Required", "请先登录账号后再连接设备。");
      return;
    }
    if (!target) this._ordinaryAccountToken = accountToken;
    const deployment = accountToken ? await this.runtime.getServerConf(accountToken) : undefined;
    if (accountToken && (generation !== this._startGeneration || !this.runtime.isCurrent(this) ||
        sessionStorage.getItem("access_token") !== accountToken)) return;
    if (!target) {
      const server = deployment?.ws_host || deployment?.id_server;
      if (!deployment || typeof server !== "string" || !server || typeof deployment.key !== "string") {
        throw new Error("服务器连接配置加载失败，请检查后台配置后重试。");
      }
      const network = {
        server,
        key: deployment.key,
      };
      if (this._ordinaryNetwork &&
          (this._ordinaryNetwork.server !== network.server || this._ordinaryNetwork.key !== network.key)) {
        this._options = undefined;
        this._password = undefined;
        this._hash = undefined;
      }
      this._ordinaryNetwork = network;
    }
    const uri = getWebClientUri(target?.server || this._ordinaryNetwork?.server || "", false);
    const signalUri = new URL(uri);
    this._id = id;
    if (!this._options) {
      this._options = this._sharedTarget ? { ...this._sharedTarget.options } : this.runtime.getPeers()[id] || {};
    }
    if (!this._sharedTarget && !this._password) {
      const p = this.getOption("password");
      if (p) {
        try {
          this._password = Uint8Array.from(JSON.parse("[" + p + "]"));
        } catch (e) {
          console.error(e);
        }
      }
    }
    this._interval = setInterval(() => {
      while (this._msgs.length) {
        this._ws?.sendMessage(this._msgs[0]);
        this._msgs.splice(0, 1);
      }
    }, 1);
    await this.loadVideoDecoder();
    if (generation !== this._startGeneration || !this.runtime.isCurrent(this)) return;
    const ws = new Websock(uri, true);
    this._ws = ws;
    await ws.open();
    if (this._ws !== ws) return;
    if (this._managedTarget && this.runtime.readManagedToken() !== requestToken) {
      this.close();
      throw managedSessionChanged;
    }

    const conn_type = rendezvous.ConnType.DEFAULT_CONN;
    const nat_type = rendezvous.NatType.SYMMETRIC;
    const key = this.getRendezvousKey() || "";
    let token: string | undefined;
    const defaultApi = sessionStorage.getItem("webclient-config-api-server");
    let relayTarget = target ?? (sessionStorage.getItem("api-server") === defaultApi ? getPublishedRelayTarget(signalUri) : undefined);
    if (this._managedTarget) {
      token = requestToken;
    } else if (accountToken && sessionStorage.getItem("access_token") === accountToken &&
               deployment?.apiServer === (window.capylinkWebClientApiServer || sessionStorage.getItem("api-server") || location.origin) &&
               deployment?.key === key && matchesRendezvous(signalUri, deployment.ws_host || deployment.id_server)) {
      token = accountToken;
      if (!target) {
        relayTarget = getPublishedRelayTarget(signalUri, {
          server: deployment.ws_host || deployment.id_server,
          relay: typeof deployment.relay_server === "string" ? deployment.relay_server : "",
        }) ?? { server: deployment.ws_host || deployment.id_server, relay: "" };
      }
    }
    const punch_hole_request = rendezvous.PunchHoleRequest.fromPartial({
      id: this.deviceId(),
      licence_key: key || undefined,
      conn_type,
      nat_type,
      token,
    });
    ws.sendRendezvous({ punch_hole_request });
    const msg = (await ws.next()) as rendezvous.RendezvousMessage;
    if (this._ws !== ws) return;
    ws.close();

    const phr = msg.punch_hole_response;
    const rr = msg.relay_response;
    if (phr) {
      if (phr?.other_failure) {
        this.msgbox("error", "Error", phr?.other_failure);
        return;
      }
      if (phr.failure != rendezvous.PunchHoleResponse_Failure.UNRECOGNIZED) {
        switch (phr?.failure) {
          case rendezvous.PunchHoleResponse_Failure.ID_NOT_EXIST:
            this.msgbox("error", "Error", "ID does not exist");
            break;
          case rendezvous.PunchHoleResponse_Failure.OFFLINE:
            this.msgbox("error", "Error", "Remote desktop is offline");
            break;
          case rendezvous.PunchHoleResponse_Failure.LICENSE_MISMATCH:
            this.msgbox("error", "Error", "Key mismatch");
            break;
          case rendezvous.PunchHoleResponse_Failure.LICENSE_OVERUSE:
            this.msgbox("error", "Error", "Key overuse");
            break;
        }
      }
    } else if (rr) {
      if (!rr.version) {
        this.msgbox("error", "Error", "Remote version is low, not support web");
        return;
      }
      await this.connectRelay(rr, relayTarget);
    }
  }

  async connectRelay(rr: rendezvous.RelayResponse, target?: RelayTarget) {
    const pk = rr.pk;
    const uri = getWebClientRelayUri(target, rr.relay_server, this._ordinaryNetwork?.server || "");
    const uuid = rr.uuid;

    const ws = new Websock(uri, false);
    this._ws = ws;
    await ws.open();
    if (this._ws !== ws) return;

    const request_relay = rendezvous.RequestRelay.fromPartial({
      licence_key: this.getRendezvousKey() || undefined,
      uuid,
    });
    ws.sendRendezvous({ request_relay });
    const requireSecure = Boolean(this._managedTarget?.key || (this._sharedTarget?.key && this._sharedTarget.passwordBase64));
    let secure = false;
    try {
      secure = await this.secure(ws, pk, requireSecure);
    } catch (error) {
      if (!requireSecure) throw error;
    }
    if (this._ws !== ws) return;
    if (requireSecure && !secure) {
      this.close();
      this.msgbox("error", "Login Error", this._sharedTarget ? "无法验证分享目标的身份，连接已关闭。" : "无法验证设备身份，连接已关闭。");
      return;
    }
    this.runtime.pushEvent("connection_ready", { secure, direct: false });
    await this.msgLoop(ws);
  }

  getRendezvousKey() {
    return (this._managedTarget ?? this._sharedTarget)?.key ?? this._ordinaryNetwork?.key;
  }

  deviceId() {
    if (!this._id.endsWith("/r") && !this._id.endsWith("\\r")) return this._id;
    const id = this._id.slice(0, -2);
    if (this._managedTarget || this._sharedTarget) return id;
    return id && !/[@?\/\\\s]/.test(id) ? id : this._id;
  }

  async secure(ws: Websock, pk: Uint8Array | undefined, required: boolean) {
    const fallback = () => {
      if (!required) ws.sendMessage({ public_key: message.PublicKey.fromPartial({}) });
      return false;
    };
    if (pk) {
      const RS_PK = "OeVuKk5nlHiXp+APNn0Y3pC1Iwpwn44JGqrQCsWqmBw=";
      try {
        pk = await crypto.verify(pk, this.getRendezvousKey() || RS_PK);
        if (pk) {
          const idpk = message.IdPk.decode(pk);
          pk = idpk.id === this.deviceId() ? idpk.pk : undefined;
        }
        if (pk?.length != 32) {
          pk = undefined;
        }
      } catch (e) {
        console.error(e);
        pk = undefined;
      }
      if (!pk)
        console.error(
          "Handshake failed: invalid public key from rendezvous server"
        );
    }
    if (!pk) {
      return fallback();
    }
    const msg = (await ws.next()) as message.Message;
    let signedId: any = msg?.signed_id;
    if (!signedId) {
      console.error("Handshake failed: invalid message type");
      return fallback();
    }
    try {
      signedId = await crypto.verify(signedId.id, Uint8Array.from(pk!));
    } catch (e) {
      console.error(e);
      return fallback();
    }
    const idpk = message.IdPk.decode(signedId);
    const id = idpk.id;
    const theirPk = idpk.pk;
    if (id != this.deviceId()) {
      console.error("Handshake failed: sign failure");
      return fallback();
    }
    if (theirPk.length != 32) {
      console.error(
        "Handshake failed: invalid public box key length from peer"
      );
      return fallback();
    }
    const [mySk, asymmetric_value] = crypto.genBoxKeyPair();
    const secret_key = crypto.genSecretKey();
    const symmetric_value = crypto.seal(secret_key, theirPk, mySk);
    const public_key = message.PublicKey.fromPartial({
      asymmetric_value,
      symmetric_value,
    });
    ws.sendMessage({ public_key });
    ws.setSecretKey(secret_key);

    return true;
  }

  async msgLoop(ws: Websock) {
    while (this._ws === ws) {
      const msg = (await ws.next()) as message.Message;
      if (this._ws !== ws) return;
      if (msg?.hash) {
        this._hash = msg?.hash;
        if (this._sharedTarget) {
          const sharedPassword = this._sharedTarget.passwordBase64;
          if (sharedPassword && !this._sharePasswordTried) {
            this._sharePasswordTried = true;
            this.loginWithPassword(decodeSharedPassword(sharedPassword));
          }
          continue;
        }
        const tmp = this.getOption('tmppwd')
        if(!this._password && tmp){
          this._password = Uint8Array.from(JSON.parse("[" + tmp + "]"));
          this.setOption('tmppwd', '')
        }
        if (!this._password)
          this.msgbox("input-password", "Password Required", "");
        this.login();
      } else if (msg?.test_delay) {
        const test_delay = msg?.test_delay;

        if (!test_delay.from_client) {
          this._ws?.sendMessage({ test_delay });
        }
      } else if (msg?.login_response) {
        const r = msg?.login_response;
        const managedPasswordAttempt = this._managedPasswordAttempt;
        this._managedPasswordAttempt = undefined;
        if (r.error) {
          if (r.error == "Wrong Password") {
            this._password = undefined;
            if (this._sharedTarget) {
              this._sharedTarget.passwordBase64 = undefined;
              this._hash = undefined;
              this.close();
              this.msgbox("error", "Login Error", sharedPasswordRejected);
              return;
            }
            if (this._managedTarget && managedPasswordAttempt) {
              this.runtime.saveManagedPassword(this._managedTarget, undefined, managedPasswordAttempt);
              delete this._options.password;
            }
            this.msgbox(
              "re-input-password",
              r.error,
              "Do you want to enter again?"
            );
          } else {
            this.msgbox("error", "Login Error", r.error);
          }
        } else if (r.peer_info) {
          this.handlePeerInfo(r.peer_info);
        }
      } else if (msg?.video_frame) {
        this.handleVideoFrame(msg?.video_frame!);
      } else if (msg?.clipboard) {
        const cb = msg?.clipboard;
        if (cb.compress) {
          const c = await decompress(cb.content);
          if (this._ws !== ws) return;
          if (!c) continue;
          cb.content = c;
        }
        try {
          this.runtime.copyToClipboard(new TextDecoder().decode(cb.content));
        } catch (e) {
          console.error(e);
        }
      } else if (msg?.cursor_data) {
        const cd = msg?.cursor_data;
        const c = await decompress(cd.colors);
        if (this._ws !== ws) return;
        if (!c) continue;
        cd.colors = c;
        this.runtime.pushEvent("cursor_data", cd);
      } else if (msg?.cursor_id) {
        this.runtime.pushEvent("cursor_id", { id: msg?.cursor_id });
      } else if (msg?.cursor_position) {
        this.runtime.pushEvent("cursor_position", msg?.cursor_position);
      } else if (msg?.misc) {
        if (!this.handleMisc(msg?.misc)) break;
      } else if (msg?.audio_frame) {
        this.runtime.playAudio(msg?.audio_frame.data);
      }
    }
  }

  msgbox(type_: string, title: string, text: string) {
    if (this.runtime.isCurrent(this)) this._msgbox?.(type_, title, text);
  }

  draw(frame: any) {
    this._draw?.(frame);
  }

  close() {
    this._startGeneration++;
    this._videoGeneration++;
    this._videoLoad?.abort();
    this._videoLoad = undefined;
    this._managedSessionCheck?.abort();
    this._managedSessionCheck = undefined;
    this._managedToken = undefined;
    this._msgs = [];
    clearInterval(this._interval);
    this._ws?.close();
    this._ws = undefined;
    this._videoDecoder?.close();
    this._videoDecoder = undefined;
  }

  refresh() {
    const misc = message.Misc.fromPartial({ refresh_video: true });
    this._ws?.sendMessage({ misc });
  }

  setMsgbox(callback: MsgboxCallback) {
    this._msgbox = callback;
  }

  setDraw(callback: DrawCallback) {
    this._draw = callback;
  }

  login(password: string | undefined = undefined) {
    if (this._sharedTarget) return;
    if (this._managedTarget) {
      const cached = this.getOption("password");
      this._managedPasswordAttempt = !password && this._password?.toString() === cached ? cached : undefined;
    }
    if (password) {
      this.loginWithPassword(password);
    } else {
      let p = this._password;
      if (p) {
        const challenge = this._hash?.challenge;
        p = hash([p, challenge!]);
      }
      this._sendLoginMessage(p);
    }
  }

  private loginWithPassword(password: string) {
    this._password = hash([password, this._hash?.salt!]);
    const response = hash([this._password, this._hash?.challenge!]);
    this.msgbox("connecting", "Connecting...", "Logging in...");
    this._sendLoginMessage(response);
  }

  async reconnect() {
    this.close();
    await this.start(this._id);
  }

  _sendLoginMessage(password: Uint8Array | undefined = undefined) {
    if (this._managedTarget && (!this._managedToken || this.runtime.readManagedToken() !== this._managedToken)) {
      this.close();
      this.msgbox("error", "Login Error", managedSessionChanged.message);
      return;
    }
    const login_request = message.LoginRequest.fromPartial({
      username: this.deviceId(),
      my_id: "web", // to-do
      my_name: "web", // to-do
      password,
      option: this.getOptionMessage(),
      video_ack_required: true,
    });
    this._ws?.sendMessage({ login_request });
  }

  getOptionMessage(): message.OptionMessage | undefined {
    let n = 0;
    const msg = message.OptionMessage.fromPartial({});
    const q = this.getImageQualityEnum(this.getImageQuality(), true);
    const yes = message.OptionMessage_BoolOption.Yes;
    if (q != undefined) {
      msg.image_quality = q;
      n += 1;
    }
    if (this._options["show-remote-cursor"]) {
      msg.show_remote_cursor = yes;
      n += 1;
    }
    if (this._options["lock-after-session-end"]) {
      msg.lock_after_session_end = yes;
      n += 1;
    }
    if (this._options["privacy-mode"]) {
      msg.privacy_mode = yes;
      n += 1;
    }
    if (this._options["disable-audio"]) {
      msg.disable_audio = yes;
      n += 1;
    }
    if (this._options["disable-clipboard"]) {
      msg.disable_clipboard = yes;
      n += 1;
    }
    return n > 0 ? msg : undefined;
  }

  sendVideoReceived() {
    const misc = message.Misc.fromPartial({ video_received: true });
    this._ws?.sendMessage({ misc });
  }

  handleVideoFrame(vf: message.VideoFrame) {
    if (!this._firstFrame) {
      this.msgbox("", "", "");
      this._firstFrame = true;
    }
    if (vf.vp9s) {
      const dec = this._videoDecoder;
      if (!dec) { this.sendVideoReceived(); return; }
      const socket = this._ws;
      const generation = this._videoGeneration;
      let i = 0;
      const n = vf.vp9s?.frames.length;
      vf.vp9s.frames.forEach((f) => {
        dec.processFrame(f.data.slice(0).buffer, (ok: any) => {
          if (generation !== this._videoGeneration || this._ws !== socket) return;
          i++;
          if (i == n) this.sendVideoReceived();
          if (ok && dec.frameBuffer && n == i) {
            this.draw(dec.frameBuffer);
          }
        });
      });
    }
  }

  handlePeerInfo(pi: message.PeerInfo) {
    this._peerInfo = pi;
    if (pi.displays.length == 0) {
      this.msgbox("error", "Remote Error", "No Display");
      return;
    }
    this.msgbox("success", "Successful", "Connected, waiting for image...");
    this.runtime.pushEvent("peer_info", pi);
    const p = this.shouldAutoLogin();
    if (p) this.inputOsPassword(p);
    const username = this.getOption("info")?.username;
    if (username && !pi.username) pi.username = username;
    this.setOption("info", pi);
    if (this.getRemember()) {
      if (this._password?.length) {
        const p = this._password.toString();
        if (p != this.getOption("password")) {
          this.setOption("password", p);
          if (this._managedTarget) this.runtime.saveManagedPassword(this._managedTarget, p);

        }
      }
    } else {
      if (this._managedTarget) this.runtime.saveManagedPassword(this._managedTarget, undefined, this.getOption("password"));
      this.setOption("password", undefined);
    }
  }

  shouldAutoLogin(): string {
    const l = this.getOption("lock-after-session-end");
    const a = !!this.getOption("auto-login");
    const p = this.getOption("os-password");
    if (p && l && a) {
      return p;
    }
    return "";
  }

  handleMisc(misc: message.Misc) {
    if (misc.audio_format) {
      this.runtime.initAudio(
        misc.audio_format.channels,
        misc.audio_format.sample_rate
      );
    } else if (misc.chat_message) {
      this.runtime.pushEvent("chat", { text: misc.chat_message.text });
    } else if (misc.permission_info) {
      const p = misc.permission_info;

      let name;
      switch (p.permission) {
        case message.PermissionInfo_Permission.Keyboard:
          name = "keyboard";
          break;
        case message.PermissionInfo_Permission.Clipboard:
          name = "clipboard";
          break;
        case message.PermissionInfo_Permission.Audio:
          name = "audio";
          break;
        default:
          return true;
      }
      this.runtime.pushEvent("permission", { [name]: p.enabled });
    } else if (misc.switch_display) {
      void this.loadVideoDecoder().catch((error) => this.msgbox("error", "Video Error", String(error)));
      this.runtime.pushEvent("switch_display", misc.switch_display);
    } else if (misc.close_reason) {
      this.msgbox("error", "Connection Error", misc.close_reason);
      this.close();
      return false;
    }
    return true;
  }

  getRemember(): Boolean {
    return this._options["remember"] || false;
  }

  setRemember(v: Boolean) {
    this.setOption("remember", v);
  }

  getOption(name: string): any {
    return this._options[name];
  }

  setOption(name: string, value: any) {
    if (value == undefined) {
      delete this._options[name];
    } else {
      this._options[name] = value;
    }
    this._options["tm"] = new Date().getTime();
    if (this._managedTarget || this._sharedTarget || this._ordinaryAccountToken !== sessionStorage.getItem("access_token") ||
        this._ordinaryNetwork?.server !== this.runtime.getNetworkOption("custom-rendezvous-server") ||
        this._ordinaryNetwork?.key !== this.runtime.getNetworkOption("key")) return;
    const peers = this.runtime.getPeers();
    peers[this._id] = this._options;
    sessionStorage.setItem("peers", JSON.stringify(peers));
  }

  inputKey(
    name: string,
    down: boolean,
    press: boolean,
    alt: Boolean,
    ctrl: Boolean,
    shift: Boolean,
    command: Boolean
  ) {
    const key_event = mapKey(name, this.runtime.isDesktop());
    if (!key_event) return;
    if (alt && (name == "VK_MENU" || name == "RAlt")) {
      alt = false;
    }
    if (ctrl && (name == "VK_CONTROL" || name == "RControl")) {
      ctrl = false;
    }
    if (shift && (name == "VK_SHIFT" || name == "RShift")) {
      shift = false;
    }
    if (command && (name == "Meta" || name == "RWin")) {
      command = false;
    }
    key_event.down = down;
    key_event.press = press;
    key_event.modifiers = this.getMod(alt, ctrl, shift, command);
    this._ws?.sendMessage({ key_event });
  }

  ctrlAltDel() {
    const key_event = message.KeyEvent.fromPartial({ down: true });
    if (this._peerInfo?.platform == "Windows") {
      key_event.control_key = message.ControlKey.CtrlAltDel;
    } else {
      key_event.control_key = message.ControlKey.Delete;
      key_event.modifiers = this.getMod(true, true, false, false);
    }
    this._ws?.sendMessage({ key_event });
  }

  inputString(seq: string) {
    const key_event = message.KeyEvent.fromPartial({ seq });
    this._ws?.sendMessage({ key_event });
  }

  switchDisplay(display: number) {
    const switch_display = message.SwitchDisplay.fromPartial({ display });
    const misc = message.Misc.fromPartial({ switch_display });
    this._ws?.sendMessage({ misc });
  }

  async inputOsPassword(seq: string) {
    this.inputMouse();
    await sleep(50);
    this.inputMouse(0, 3, 3);
    await sleep(50);
    this.inputMouse(1 | (1 << 3));
    this.inputMouse(2 | (1 << 3));
    await sleep(1200);
    const key_event = message.KeyEvent.fromPartial({ press: true, seq });
    this._ws?.sendMessage({ key_event });
  }

  lockScreen() {
    const key_event = message.KeyEvent.fromPartial({
      down: true,
      control_key: message.ControlKey.LockScreen,
    });
    this._ws?.sendMessage({ key_event });
  }

  getMod(alt: Boolean, ctrl: Boolean, shift: Boolean, command: Boolean) {
    const mod: message.ControlKey[] = [];
    if (alt) mod.push(message.ControlKey.Alt);
    if (ctrl) mod.push(message.ControlKey.Control);
    if (shift) mod.push(message.ControlKey.Shift);
    if (command) mod.push(message.ControlKey.Meta);
    return mod;
  }

  inputMouse(
    mask: number = 0,
    x: number = 0,
    y: number = 0,
    alt: Boolean = false,
    ctrl: Boolean = false,
    shift: Boolean = false,
    command: Boolean = false
  ) {
    const mouse_event = message.MouseEvent.fromPartial({
      mask,
      x,
      y,
      modifiers: this.getMod(alt, ctrl, shift, command),
    });
    this._ws?.sendMessage({ mouse_event });
  }

  toggleOption(name: string) {
    const v = !this._options[name];
    const option = message.OptionMessage.fromPartial({});
    const v2 = v
      ? message.OptionMessage_BoolOption.Yes
      : message.OptionMessage_BoolOption.No;
    switch (name) {
      case "show-remote-cursor":
        option.show_remote_cursor = v2;
        break;
      case "disable-audio":
        option.disable_audio = v2;
        break;
      case "disable-clipboard":
        option.disable_clipboard = v2;
        break;
      case "lock-after-session-end":
        option.lock_after_session_end = v2;
        break;
      case "privacy-mode":
        option.privacy_mode = v2;
        break;
      case "block-input":
        option.block_input = message.OptionMessage_BoolOption.Yes;
        break;
      case "unblock-input":
        option.block_input = message.OptionMessage_BoolOption.No;
        break;
      default:
        return;
    }
    if (name.indexOf("block-input") < 0) this.setOption(name, v);
    const misc = message.Misc.fromPartial({ option });
    this._ws?.sendMessage({ misc });
  }

  getImageQuality() {
    return this.getOption("image-quality");
  }

  getImageQualityEnum(
    value: string,
    ignoreDefault: Boolean
  ): message.ImageQuality | undefined {
    switch (value) {
      case "low":
        return message.ImageQuality.Low;
      case "best":
        return message.ImageQuality.Best;
      case "balanced":
        return ignoreDefault ? undefined : message.ImageQuality.Balanced;
      default:
        return undefined;
    }
  }

  setImageQuality(value: string) {
    this.setOption("image-quality", value);
    const image_quality = this.getImageQualityEnum(value, false);
    if (image_quality == undefined) return;
    const option = message.OptionMessage.fromPartial({ image_quality });
    const misc = message.Misc.fromPartial({ option });
    this._ws?.sendMessage({ misc });
  }

  async loadVideoDecoder() {
    const generation = ++this._videoGeneration;
    this._videoLoad?.abort();
    const pending = new AbortController();
    this._videoLoad = pending;
    this._videoDecoder?.close();
    this._videoDecoder = undefined;
    try {
      const decoder = await loadVp9(undefined, pending.signal);
      if (generation !== this._videoGeneration) { decoder.close(); return; }
      this._videoDecoder = decoder;
    } catch (error) {
      if (generation === this._videoGeneration) throw error;
    } finally {
      if (this._videoLoad === pending) this._videoLoad = undefined;
    }
  }

}

function getPublishedRelayTarget(uri: URL, target = window.capylinkWebClientRelayTarget): RelayTarget | undefined {
  if (!target || !matchesRendezvous(uri, target.server)) return undefined;
  try {
    if (target.relay) getWebClientUri(target.relay, true, true);
    if (target.relay || websocketScheme.test(target.server)) return target;
  } catch {
    // Invalid deployment configuration cannot replace the announced relay.
  }
  return undefined;
}

function configuredWebRelay(target?: RelayTarget): URL | undefined {
  if (!target || !websocketScheme.test(target.relay)) return undefined;
  try {
    return new URL(getWebClientUri(target.relay, true, true));
  } catch {
    return undefined;
  }
}

function webSocketPort(uri: URL): number {
  return Number(uri.port || (uri.protocol === "wss:" ? 443 : 80));
}

function getWebClientRelayUri(target: RelayTarget | undefined, relayServer: string, defaultServer: string): string {
  if (websocketScheme.test(relayServer)) {
    try {
      return getWebClientUri(relayServer, true, true);
    } catch (error) {
      const configured = configuredWebRelay(target);
      if (configured && error === mixedContentWebSocketError) {
        const advertised = new URL(relayServer);
        const standardRawRelay = advertised.pathname === "/" && webSocketPort(advertised) === PORT + 3;
        const sameListener = configured.pathname === advertised.pathname && webSocketPort(configured) === webSocketPort(advertised);
        if (configured.hostname === advertised.hostname && (standardRawRelay || sameListener)) return configured.href;
      }
      throw error;
    }
  }
  if (!relayServer) {
    if (!target) return getWebClientUri(defaultServer, true);
    return target.relay ? getWebClientUri(target.relay, true, true) : getWebClientUri(target.server, true);
  }
  const advertisedUri = getWebClientUri(relayServer, true, true);
  if (!target) return advertisedUri;
  const advertised = new URL(advertisedUri);
  const configured = configuredWebRelay(target);
  if (configured) {
    // A configured proxy can stand in for this host's standard relay or the same WS listener.
    return configured.hostname === advertised.hostname &&
      (webSocketPort(advertised) === PORT + 3 || webSocketPort(configured) === webSocketPort(advertised))
      ? configured.href : advertisedUri;
  }
  if (target.relay || !websocketScheme.test(target.server)) return advertisedUri;
  const idProxy = new URL(target.server);
  return proxyIdPath.test(idProxy.pathname) && advertised.hostname === idProxy.hostname && webSocketPort(advertised) === PORT + 3
    ? getWebClientUri(target.server, true) : advertisedUri;
}

function hash(datas: (string | Uint8Array)[]): Uint8Array {
  const hasher = new sha256.Hash();
  datas.forEach((data) => {
    if (typeof data == "string") {
      data = new TextEncoder().encode(data);
    }
    return hasher.update(data);
  });
  return hasher.digest();
}
