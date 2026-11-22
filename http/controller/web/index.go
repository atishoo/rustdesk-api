package web

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
)

type Index struct {
}

func (i *Index) Index(c *gin.Context) {
	c.Redirect(302, "/_admin/")
}

func (i *Index) ConfigJs(c *gin.Context) {
	config := global.Config.Capylink
	apiServer, _ := json.Marshal(config.ApiServer)
	shareApiServer, _ := json.Marshal(config.WebclientShareApiServer)
	idServer, _ := json.Marshal(config.IdServer)
	relayServer, _ := json.Marshal(config.RelayServer)
	key, _ := json.Marshal(config.Key)
	wsHost, _ := json.Marshal(config.WsHost)
	script := fmt.Sprintf(`const webClientApiServer = %s;
const webClientServer = { server: %s, relay: %s };
const webClientBuiltIn = window.location.pathname === '/webclient/';
const webClientLaunch = new URLSearchParams(window.location.hash.startsWith('#/?') ? window.location.hash.slice(3) : '');
const webClientGuest = webClientLaunch.has('share_token');
const webClientManaged = !webClientGuest && webClientLaunch.get('managed') === '1';
const webClientShareApiServer = %s;
window.capylinkWebClientApiServer = webClientGuest && webClientShareApiServer
    ? webClientShareApiServer
    : (webClientBuiltIn ? window.location.origin : webClientApiServer);
if (!webClientGuest && !webClientManaged) {
    window.capylinkWebClientAccountApiServer = webClientBuiltIn ? window.location.origin : webClientApiServer;
    window.capylinkWebClientRelayTarget = webClientServer;
}

window.webclient_magic_queryonline = %d;
window.ws_host = %s;

if (webClientBuiltIn && webClientManaged) {
    const id = webClientLaunch.get('id');
    if (id) {
        window.capylinkManagedTarget = { id, ...webClientServer, key: %s };
    }
}
`, apiServer, idServer, relayServer, shareApiServer, config.WebclientMagicQueryonline, wsHost, key)

	c.Header("Content-Type", "application/javascript")
	c.Header("Cache-Control", "no-store")
	c.String(200, script)
}
