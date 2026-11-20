import {matchesRendezvous, translate} from './common'

window._gwen = {}
window._gwen.kv = {}

function stringToUint8Array(str) {
    var arr = [];
    for (var i = 0, j = str.length; i < j; ++i) {
        arr.push(str.charCodeAt(i));
    }

    var tmpUint8Array = new Uint8Array(arr);
    return tmpUint8Array
}

function getQueryVariable() {
    const query = window.location.hash.substring(3);
    const vars = query.split("&");
    for (var i = 0; i < vars.length; i++) {
        var pair = vars[i].split("=");
        window._gwen.kv[pair[0]] = pair[1]
    }
}

getQueryVariable()

const launchParams = () => new URLSearchParams(window.location.hash.startsWith('#/?') ? window.location.hash.slice(3) : '')
const launch = launchParams()
export const sharedLaunch = launch.has('share_token')
export const managedLaunch = launch.get('managed') === '1' && !sharedLaunch
const shareToken = launch.get('share_token')
const launchSession = launch.get('launch_session') || ''

export const ordinaryNetworkOptions = new Set(['custom-rendezvous-server', 'relay-server', 'key'])

export function clearWebClientAccount() {
    for (const key of ['access_token', 'access_token_deployment', 'user_info', 'account-id-server', 'account-key', 'peers']) {
        sessionStorage.removeItem(key)
    }
}

export function setWebClientAccountApi(apiServer) {
    clearWebClientAccount()
    sessionStorage.setItem('api-server', apiServer)
}

export function getWebClientNetworkOption(name) {
    const value = sessionStorage.getItem(name)
    if (name === 'key') {
        const manualServer = sessionStorage.getItem('custom-rendezvous-server')
        if (value || (value === '' && manualServer)) return value
        if (!manualServer || matchesRendezvous(manualServer, sessionStorage.getItem('account-id-server'))) return sessionStorage.getItem('account-key')
        return null
    }
    return value ?? (name === 'custom-rendezvous-server' ? sessionStorage.getItem('account-id-server') : null)
}

if (!managedLaunch && !sharedLaunch) {
    const configuredApi = window.capylinkWebClientAccountApiServer || location.origin
    if (sessionStorage.getItem('webclient-config-api-server') !== configuredApi) {
        setWebClientAccountApi(configuredApi)
        sessionStorage.setItem('webclient-config-api-server', configuredApi)
    }
}

export function readManagedLaunchToken() {
    if (!/^[a-f0-9]{32}$/.test(launchSession)) return undefined
    try {
        const session = JSON.parse(localStorage.getItem('capylink-web-token') || 'null')
        if (session?.origin === location.origin && session.launchSession === launchSession && typeof session.token === 'string') {
            return session.token || undefined
        }
    } catch {
        // A malformed session cannot be reused.
    }
    return undefined
}

let id = window._gwen.kv.id || ''
if (id && !managedLaunch && !sharedLaunch) {
    try {
        id = decodeURIComponent(id)
    } catch {
        // 保留无法解码的原始 ID，继续处理其他参数。
    }
    localStorage.setItem('remote-id', id)
}
export let sharedTarget
window.capylinkSharedTargetReady = shareToken?.trim() ? redeemSharedTarget() : null
window.addEventListener('hashchange', () => {
    const next = launchParams()
    const nextToken = next.get('share_token')
    const changedShare = nextToken !== null && nextToken !== shareToken
    const changedManaged = nextToken === null && next.get('managed') === '1' &&
        (!managedLaunch || next.get('id') !== launch.get('id') || (next.get('launch_session') || '') !== launchSession)
    const changedOrdinary = nextToken === null && next.get('managed') !== '1' &&
        (sharedLaunch || managedLaunch || (next.has('id') && next.get('id') !== launch.get('id')))
    if (changedShare || changedManaged || changedOrdinary) {
        sharedTarget = undefined
        window.curConn?.close()
        window.location.reload()
    }
})

export function decodeSharedPassword(encoded) {
    return new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(stringToUint8Array(window.atob(encoded)))
}

async function redeemSharedTarget() {
    const apiServer = window.capylinkWebClientApiServer
    if (typeof apiServer !== 'string') return null
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort(), 15000)
    try {
        const response = await fetch(apiServer + '/api/shared-peer', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ share_token: shareToken }),
            signal: controller.signal,
        })
        if (!response.ok) return null
        const { code, data } = await response.json()
        if (code !== 0 || typeof data?.id_server !== 'string' || !data.id_server || typeof data.key !== 'string') return null
        const peer = data.peer
        const id = peer?.info?.id
        if (typeof id !== 'string' || !id || typeof peer.tmppwd !== 'string') return null
        decodeSharedPassword(peer.tmppwd)
        const currentToken = launchParams().get('share_token')
        if (currentToken !== null && currentToken !== shareToken) return null
        sharedTarget = {
            id,
            server: data.id_server,
            key: data.key,
            relay: typeof data.relay_server === 'string' ? data.relay_server : '',
            passwordBase64: peer.tmppwd,
            options: { info: peer.info },
        }
        return sharedTarget
    } catch {
        return null
    } finally {
        window.clearTimeout(timeout)
    }
}

const managedPasswordKey = (target) => 'capylink-managed-peer-password:' + JSON.stringify([launchSession, location.origin, target.server, target.key, target.id])

export function loadManagedPassword(target) {
    const password = localStorage.getItem(managedPasswordKey(target))
    return password ? { password, remember: true } : {}
}

export function saveManagedPassword(target, password, expectedPassword) {
    const key = managedPasswordKey(target)
    const update = () => {
        if (!readManagedLaunchToken()) return
        if (typeof password === 'string' && password) {
            localStorage.setItem(key, password)
            if (!readManagedLaunchToken()) localStorage.removeItem(key)
        } else if (typeof expectedPassword === 'string' && localStorage.getItem(key) === expectedPassword) localStorage.removeItem(key)
    }
    if (navigator.locks) void navigator.locks.request(key, update).catch(() => undefined)
    else update()
}

let serverConfigRequest
export function getServerConf(token) {
    const apiServer = sessionStorage.getItem('api-server') || location.origin
    if (!token || !apiServer || managedLaunch || sharedLaunch) {
        return
    }
    if (serverConfigRequest?.token === token && serverConfigRequest.apiServer === apiServer) {
        return serverConfigRequest.result
    }
    const result = fetch(apiServer.replace(/\/+$/, '') + "/api/server-config", {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            redirect: 'error',
            signal: AbortSignal.timeout(5000),
        }
    ).then(res => res.ok ? res.json() : undefined).then(res => {
        if (res?.code === 0 && sessionStorage.getItem('access_token') === token &&
            (sessionStorage.getItem('api-server') || location.origin) === apiServer) {
            if (typeof res.data.id_server === 'string' && typeof res.data.key === 'string') {
                const previousServer = getWebClientNetworkOption('custom-rendezvous-server')
                const previousKey = getWebClientNetworkOption('key')
                sessionStorage.setItem('account-id-server', res.data.id_server)
                sessionStorage.setItem('account-key', res.data.key)
                if (previousServer !== getWebClientNetworkOption('custom-rendezvous-server') ||
                    previousKey !== getWebClientNetworkOption('key')) sessionStorage.removeItem('peers')
            }
            if (res.data.peers && matchesRendezvous(getWebClientNetworkOption('custom-rendezvous-server') || '', res.data.id_server) &&
                getWebClientNetworkOption('key') === res.data.key) importServerPeers(res.data.peers)
            return { ...res.data, apiServer }
        }
    }).catch(() => undefined).finally(() => {
        if (serverConfigRequest?.result === result) serverConfigRequest = undefined
    })
    serverConfigRequest = { apiServer, token, result }
    return result
}

function importServerPeers(peers) {
    try {
        const saved = Object.assign(Object.create(null), JSON.parse(sessionStorage.getItem('peers')))
        let added = false
        for (const [id, peer] of Object.entries(peers)) {
            if (!saved[id]) {
                saved[id] = peer
                added = true
            } else {
                saved[id].info = peer.info
            }
            if (peer.info?.hash && !saved[id].password) {
                try {
                    const decoded = window.atob(peer.info.hash)
                    saved[id].password = stringToUint8Array(decoded).toString()
                    saved[id].remember = true
                } catch {
                    // One invalid password must not block other devices.
                }
            }
        }
        sessionStorage.setItem('peers', JSON.stringify(saved))
        if (added && !window.curConn) window.location.reload()
    } catch {
        // Import is optional. It must not invalidate the authenticated server configuration.
    }
}

const brandName = translate(navigator.language, "capylink");
document.title = brandName;
document.querySelector('meta[name="apple-mobile-web-app-title"]')?.setAttribute("content", brandName);
