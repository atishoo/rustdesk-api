# capylink API

Local accounts use email as the only password-login identifier. Profiles support nickname editing and avatar uploads. Avatars are stored in the API `data/avatars` directory, which must be persisted with API data. LDAP password login also searches by email. External OAuth auto-registration requires a valid email; display names or email prefixes provide the initial nickname.

This project implements the capylink API using Go, and includes both a web UI and web client. capylink is a remote
desktop software that provides self-hosted solutions.

<div align=center>
<img src="https://img.shields.io/badge/golang-1.22-blue"/>
<img src="https://img.shields.io/badge/gin-v1.9.0-lightBlue"/>
<img src="https://img.shields.io/badge/gorm-v1.25.7-green"/>
<img src="https://img.shields.io/badge/swag-v1.16.3-yellow"/>
<img src="https://goreportcard.com/badge/github.com/lejianwen/rustdesk-api/v2"/>
<img src="https://github.com/lejianwen/rustdesk-api/actions/workflows/build.yml/badge.svg"/>
</div>

## Better used with [lejianwen/rustdesk-server].
> [lejianwen/rustdesk-server] is a fork of the upstream RustDesk Server repository.
> 1. Solves the API connection timeout issue.
> 2. Can enforce login before initiating a connection.
> 3. Supports client websocket.


# Features

- PC API
    - Personal API
    - Login
    - Address Book
    - Authorized login, 
      - supports `GitHub`, `Google` and `OIDC` login, 
      - supports `web admin` authorized login, 
      - supports LDAP (tested with AD and OpenLDAP), enabled in management console service settings
    - i18n
- Web Admin
    - User Management
    - Device Management
    - Address Book Management
    - Tag Management
    - OAuth Management
    - Service settings, including LDAP
    - Login Logs
    - Connection Logs
    - File Transfer Logs
    - Quick access to web client
    - i18n
    - Share to guest by web client
    - Server control (some simple official commands [WIKI](https://github.com/lejianwen/rustdesk-api/wiki/Rustdesk-Command))
- Web Client
    - Automatically obtain API server
    - Automatically obtain ID server and KEY
    - Automatically obtain address book
    - Visitors are remotely to the device via a temporary sharing link
- CLI
    - Reset admin password

## Overview

### API Service
Basic implementation of the PC client's primary interfaces.Supports the Personal version api, which can be enabled by configuring the `capylink.personal` file or the `CAPYLINK_API_CAPYLINK_PERSONAL` environment variable.

Configure the deployment API address, then sign in from the client account page. After signing in, use address books to access authorized devices.

### IP location

The API uses the [official ip2region Go implementation](https://github.com/lionsoul2014/ip2region/tree/cba49ef11b4051bc25334a3c5712715d636de90d/binding/golang) and bundled offline IPv4/IPv6 databases. Login, connection and file audit records, and device heartbeats store a location snapshot together with their IP. Operational blacklist, blocklist, usage, IP blocker and IP change commands also persist observed addresses in `ip_locations`. The console displays these server-computed values. Server addresses, trusted proxies and direct connection targets retain their connection semantics. Existing records are not backfilled.

The location contains available country, province, city and ISP data. Private and loopback addresses are labelled separately; invalid or unlisted addresses are unknown, and empty IPs have no location. Audits retain the reported client IP; login and heartbeat use the request IP under the existing trusted proxy policy.

Fresh installations create the current schema at startup. Existing databases are never automatically altered or upgraded by version. Existing deployments need their missing schema completed separately; do not change `versions` or convert/backfill old records. New location columns default to an empty string; subsequent device heartbeats update the device location.

The `gin.resources-path` directory must contain `ip2region/ip2region_v4.xdb` and `ip2region/ip2region_v6.xdb`; missing or invalid databases prevent startup. Standard releases and images include both via the existing resources packaging. They are validated and loaded into memory once (approximately 46 MiB); restart the API after updating them.

Both databases and the Go dependency are pinned to upstream commit `cba49ef11b4051bc25334a3c5712715d636de90d`. Data comes from its `data/` directory under Apache-2.0 or MIT; the upstream notice is included in `resources/ip2region/LICENSE.md`. Replace both databases from the same official version and verify real queries when updating. Current SHA-256 values:

- `ip2region_v4.xdb`: `4ed5d3398f0a72346a6cec780c57601bb6977fa4f29e9682cd60a66e3478c542`
- `ip2region_v6.xdb`: `1f0be1ffbbc3e4eeef10f354e01355f22f57ec649a496a17fd602539f3d384c1`

### Web Admin

* The frontend and backend are separated to provide a user-friendly management interface, primarily for managing and
displaying data. Build the console from the companion Web repository. [capylink-web](https://github.com/atishoo/rustdesk-web)

* Admin panel URL: `http://<your server[:port]>/_admin/`
* A fresh installation does not create a preset administrator. With registration enabled, the first account that completes email verification becomes the super administrator.


1. Administrators can use the sidebar to manage users, devices, address books, and service configuration.
2. Regular users can view their devices, address books, shares, and login logs.

3. You can directly launch the client or open the Web Client for convenience; you can also create share links, which require the recipient to sign in before using Web Client remote access.
4. OAuth support: Currently, `GitHub`, `Google` and `OIDC`  are supported. You need to create an `OAuth App` and configure it in
   the admin panel.
    - For `Google` and `Github`, you don't need to fill the `Issuer` and `Scpoes`
    - For `OIDC`, you must set the `Issuer`. And `Scopes` is optional which default is `openid,email,profile`, please make sure this `Oauth App` can access `sub`, `email` and `preferred_username`
    - Create a `GitHub OAuth App`
      at `Settings` -> `Developer settings` -> `OAuth Apps` -> `New OAuth App` [here](https://github.com/settings/developers).
    - Set the `Authorization callback URL` to `http://<your server[:port]>/api/oidc/callback`,
      e.g., `http://127.0.0.1:21114/api/oidc/callback`.
   
5. Login logs
6. Connection logs
7. File transfer logs
10. Service configuration and commands

    - **Service configuration**: Open **服务配置** in the sidebar to inspect the API, ID Server, Relay Server, and console settings.
    - **Simple commands**: Open **服务命令** → **简单** to read or update service parameters using forms.
    - **Advanced commands**: Open **服务命令** → **高级** to select a target service, send commands, and manage custom commands.

11. **LDAP support**. Once LDAP is configured in the management console service settings, users can sign in with directory accounts (tested with AD and OpenLDAP). To enable LDAP or change an enabled configuration, sign in as a local administrator outside the LDAP user search and enter that administrator’s current password. Synchronization applies directory administrator role removals even when only one administrator remains. If the directory verifies the credentials but the role lookup or synchronization fails, the sign-in is denied.
  
### Web Client:

The console uses the WebSocket endpoint (`ws_host`, falling back to `id_server`), public key, and Relay configuration returned by the API. After verifying the current account session, endpoint, and public key, both WS and WSS signaling carry the verified account token. The final request to the relay never includes the account token.

1. If you're already logged into the admin panel, the web client will log in automatically.
2. If you're not logged in, simply click the login button in the top right corner, and the API server will be
   pre-configured.
3. After logging in, the ID server and key will be automatically synced.
4. The address book will also be automatically saved to the web client for convenient use.

The built-in `/webclient/` uses its own origin for account login and configuration. On a reverse proxy, forward `/webclient/`, `/webclient-config/`, and `/api/` under the same domain.

Share links redeem through the Web Client origin at `/api/shared-peer` by default. If a standalone Web Client does not proxy that route, set `capylink.webclient-share-api-server` or `CAPYLINK_API_CAPYLINK_WEBCLIENT_SHARE_API_SERVER` to the API base URL, such as `https://api.example.com/capylink`. Serve or proxy `/webclient-config/index.js` to load the share redemption settings from that API. For a cross-origin deployment, allow the standalone Web Client origin on `/api/shared-peer` at your API gateway.

### Automated Documentation : API documentation is generated using Swag, making it easier for developers to understand and use the API.

1. Admin panel docs: `<your server[:port]>/admin/swagger/index.html`
2. PC client docs: `<your server[:port]>/swagger/index.html`
   ![api_swag](docs/api_swag.png)

### CLI
```bash
# help
./apimain -h
```

#### Reset admin password
```bash
./apimain reset-admin-pwd <pwd>
```

## Installation and Setup

### Configuration

* [Config File](./conf/config.yaml)
* Modify the configuration in `conf/config.yaml`. 
* If `gorm.type` is set to `sqlite`, MySQL-related configurations are not required.
* Language support: `en` and `zh-CN` are supported. The default is `zh-CN`.

After adding the OAuth `proxy_url` field, existing databases require the deployment operator to add a `proxy_url TEXT` column to `oauths` manually. New databases create the field from the current model.

Open LDAP settings under Service Settings in the management console. Changes take effect when saved. New databases initialize the LDAP tables and user fields. Before starting an upgraded deployment, make sure the schema includes the `ldap_settings` and `ldap_account_verifications` tables and the `users.ldap_synced` and `users.ldap_permission_revision` fields as defined in the models, then enter the LDAP settings in the console. [LDAP model](./model/ldapSetting.go) · [User model](./model/user.go)

Password login through Vite's default same-origin proxy works with an empty origin allowlist. To call the API across origins during local development or use WebAuth or OIDC from Vite, explicitly list the development origin, such as `http://127.0.0.1:5174`, in the local API's `app.admin-origins` setting or `CAPYLINK_API_APP_ADMIN_ORIGINS` environment variable.

When third-party OAuth login is enabled, the API connects to each provider directly by default. If a provider requires an HTTP(S) egress proxy, set `proxy_url` in that provider's OAuth configuration.

Admin OAuth requests require an absolute `redirect_url`. Its origin must match the request origin and appear in `app.admin-origins` or match `capylink.api-server`. For same-origin reverse proxy deployments, preserve the original `Host`, set `X-Forwarded-Proto` to the public scheme, and list the immediate proxy address in `gin.trust-proxy` or `CAPYLINK_API_GIN_TRUST_PROXY`.

Web Client connections always require login. Share links can still be created, but anyone opening one must sign in. The ID Server receives the login token and can validate its account identity when the shared JWT key is configured.


### Environment Variables
The environment variables correspond one-to-one with the configurations in the `conf/config.yaml` file. The prefix for variable names is `CAPYLINK_API`.
The table below does not list all configurations. Please refer to the configurations in `conf/config.yaml`.

Upgrade existing configuration from the `rustdesk` root to `capylink`, and rename the `RUSTDESK_API` environment prefix to `CAPYLINK_API`; the server configuration segment also becomes `CAPYLINK`. Existing SQLite database files remain in use; new deployments use `data/capylinkapi.db`.

| Variable Name                                          | Description                                                                                                                                         | Example                       |
|--------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------|
| TZ                                                     | timezone                                                                                                                                            | Asia/Shanghai                 |
| CAPYLINK_API_LANG                                      | Language                                                                                                                                            | `en`,`zh-CN`                  |
| CAPYLINK_API_APP_WEB_CLIENT                            | web client on/off; 1: on, 0 off, default: 1                                                                                                         | 1                             |
| CAPYLINK_API_APP_ADMIN_ORIGINS                         | Comma-separated trusted console origins. The list is empty by default. | `http://127.0.0.1:5174` |
| CAPYLINK_API_APP_SHOW_SWAGGER                          | swagger visible; 1: yes, 0: no; default: 0                                                                                                          | `0`                           |
| CAPYLINK_API_APP_TOKEN_EXPIRE                          | token expire duration                                                                                                                               | `168h`                        |
| CAPYLINK_API_APP_CAPTCHA_THRESHOLD                     | Use -1 to disable CAPTCHA or 0 to always require it. A positive threshold must be lower than the ban threshold when both are enabled. | `-1` |
| CAPYLINK_API_APP_BAN_THRESHOLD                         | Block password login and related requests for 30 minutes after this many failures from the same IP within 10 minutes. Use 0 to disable the ban. | `10` |
| CAPYLINK_API_SMTP_HOST                                 | SMTP server address                                                                                                                                 | `smtp.aliyun.com`             |
| CAPYLINK_API_SMTP_USER                                 | SMTP username                                                                                                                                       | `easyvcode@aliyun.com`        |
| CAPYLINK_API_SMTP_PASSWORD                             | SMTP password                                                                                                                                       |                               |
| CAPYLINK_API_SMTP_PORT                                 | SMTP port                                                                                                                                           | `465`                         |
| CAPYLINK_API_SMTP_CRYPTO                               | SMTP encryption mode, use `ssl` for port 465                                                                                                        | `ssl`                         |
| ----- ADMIN Configuration-----                         | ----------                                                                                                                                          | ----------                    |
| CAPYLINK_API_ADMIN_TITLE                               | Admin Title                                                                                                                                         | `capylink Api Admin`          |
| ----- GIN Configuration -----                          | ---------------------------------------                                                                                                             | ----------------------------- |
| CAPYLINK_API_GIN_TRUST_PROXY                           | Trusted proxy IPs, separated by commas.                                                                                                             | 192.168.1.2,192.168.1.3       |
| ----- GORM Configuration -----                         | ---------------------------------------                                                                                                             | ----------------------------- |
| CAPYLINK_API_GORM_TYPE                                 | Database type (`sqlite` or `mysql`). Default is `sqlite`.                                                                                           | sqlite                        |
| CAPYLINK_API_GORM_MAX_IDLE_CONNS                       | Maximum idle connections                                                                                                                            | 10                            |
| CAPYLINK_API_GORM_MAX_OPEN_CONNS                       | Maximum open connections                                                                                                                            | 100                           |
| CAPYLINK_API_CAPYLINK_PERSONAL                         | Open Personal Api 1:Enable,0:Disable                                                                                                                | 1                             |
| ----- MYSQL Configuration -----                        | ---------------------------------------                                                                                                             | ----------------------------- |
| CAPYLINK_API_MYSQL_USERNAME                            | MySQL username                                                                                                                                      | root                          |
| CAPYLINK_API_MYSQL_PASSWORD                            | MySQL password                                                                                                                                      | 111111                        |
| CAPYLINK_API_MYSQL_ADDR                                | MySQL address                                                                                                                                       | 192.168.1.66:3306             |
| CAPYLINK_API_MYSQL_DBNAME                              | MySQL database name                                                                                                                                 | capylink                      |
| CAPYLINK_API_MYSQL_TLS                             | Whether to enable TLS, optional values: `true`, `false`, `skip-verify`, `custom` | `false`                       |
| ----- CAPYLINK Configuration -----                     | ---------------------------------------                                                                                                             | ----------------------------- |
| CAPYLINK_API_CAPYLINK_ID_SERVER                        | capylink ID server address                                                                                                                          | 192.168.1.66:21116            |
| CAPYLINK_API_CAPYLINK_RELAY_SERVER                     | capylink relay server address                                                                                                                       | 192.168.1.66:21117            |
| CAPYLINK_API_CAPYLINK_API_SERVER                       | capylink API server address                                                                                                                         | http://192.168.1.66:21114     |
| CAPYLINK_API_CAPYLINK_KEY                              | capylink key                                                                                                                                        | 123456789                     |
| CAPYLINK_API_CAPYLINK_KEY_FILE                         | capylink key file                                                                                                                                   | `./conf/data/id_ed25519.pub`  |
| CAPYLINK_API_CAPYLINK<br/>_WEBCLIENT_MAGIC_QUERYONLINE | New online query method is enabled in the web client v2; '1': Enabled, '0': Disabled, not enabled by default                                        | `0`                           |
| CAPYLINK_API_CAPYLINK_WS_HOST                          | Custom Websocket Host                                                                                                                               | `wss://192.168.1.123:1234`    |
| ----JWT----                                            | --------                                                                                                                                            | --------                      |
| CAPYLINK_API_JWT_KEY                                   | Custom JWT key used to validate ID Server login tokens; an empty value disables JWT validation. |                               |
| CAPYLINK_API_JWT_EXPIRE_DURATION                       | JWT expire duration                                                                                                                                 | `168h`                        |

### Installation Steps

#### Running via Docker

1. Run directly with Docker. Configuration can be modified by mounting the config file `/app/conf/config.yaml`, or by
   using environment variables to override settings.
    
    ```bash
    docker run -d --name capylink-api -p 21114:21114 \
    -v /data/capylink/api:/app/data \
    -e CAPYLINK_API_LANG=en \
    -e CAPYLINK_API_CAPYLINK_ID_SERVER=192.168.1.66:21116 \
    -e CAPYLINK_API_CAPYLINK_RELAY_SERVER=192.168.1.66:21117 \
    -e CAPYLINK_API_CAPYLINK_API_SERVER=http://192.168.1.66:21114 \
    -e CAPYLINK_API_CAPYLINK_KEY=abc123456 \
    ghcr.io/atishoo/capylink-api:ultrapro
    ```

2. Using `docker-compose`,look [WIKI](https://github.com/lejianwen/rustdesk-api/wiki)

#### Running from Release

Download the release from [release](https://github.com/lejianwen/rustdesk-api/releases).

#### Source Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/atishoo/rustdesk-api.git
   cd rustdesk-api
   ```

2. Install dependencies:

    ```bash
    go mod tidy
    # Install Swag if you need to generate documentation; otherwise, you can skip this step
    go install github.com/swaggo/swag/cmd/swag@latest
    ```

3. Build the sibling console checkout with Node.js 22. [capylink-web](https://github.com/atishoo/rustdesk-web)
   ```bash
   (cd ../rustdesk-web && npm ci && npm run build)
   mkdir -p resources/admin
   cp -a ../rustdesk-web/dist/. resources/admin/
   ```
   GitHub Actions builds `rustdesk-web` under the same owner by default. Use the repository variables `WEBCLIENT_REPOSITORY` and `WEBCLIENT_REF` to select a matching console revision. For a private console repository, set `RUSTDESK_WEB_READ_TOKEN` in the API repository with read access to the console contents. Publish the matching console revision before releasing the API.

   To build the development image locally, run this from the API repository root.
   ```bash
   docker build -f Dockerfile.dev --build-context capylink-web=../rustdesk-web .
   ```
   For a branded client, set `VITE_NATIVE_CLIENT_SCHEME` to its registered protocol name in GitHub Actions repository variables, the Compose environment, or `docker build --build-arg`.

4. Run:
    ```bash
    # Run directly
   go run cmd/apimain.go
   # Or generate and run the API using generate_api.go
   go generate generate_api.go
   ```
   > **Note:** When using `go run` or the compiled binary, the `conf` and `resources`
   > directories must exist relative to the current working directory. If you run
   > the program from another location, specify absolute paths with `-c` and the
   > `CAPYLINK_API_GIN_RESOURCES_PATH` environment variable. Example:
   > ```bash
   > CAPYLINK_API_GIN_RESOURCES_PATH=/opt/capylink-api/resources ./apimain -c /opt/capylink-api/conf/config.yaml
   > ```

5. To compile, change to the project root directory. For Windows, run `build.bat`, and for Linux, run `build.sh`. After
   compiling, the corresponding executables will be generated in the `release` directory. Run the compiled executables
   directly.

6. Open `http://<your server[:port]>/_admin/` and complete email verification registration. The first registered account becomes the super administrator.

#### Running the UltraPro API + Web image

After the UltraPro branch build completes, deploy the multi-architecture image `ghcr.io/atishoo/capylink-api:ultrapro`. It contains the API and management console. Run ID Server and Relay Server separately with `ghcr.io/atishoo/capylink-server:ultrapro`. For private GHCR packages, run `docker login ghcr.io` before starting the existing Compose deployment.

#### Running with Compose

Use this repository's `docker-compose.yaml` to run the API, management console, and separate ID Server and Relay Server. Environment variable names are listed in that file and the configuration table above.

## Others

- [WIKI](https://github.com/lejianwen/rustdesk-api/wiki)
- [Connection Timeout](https://github.com/lejianwen/rustdesk-api/issues/92)
- [Change client ID](https://github.com/abdullah-erturk/RustDesk-ID-Changer)
- [Web client source](https://hub.docker.com/r/keyurbhole/flutter_web_desk)

## Acknowledgements

Thanks to everyone who contributed!

<a href="https://github.com/lejianwen/rustdesk-api/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=lejianwen/rustdesk-api" />
</a>

## Thanks for your support! If you find this project useful, please give it a ⭐️. Thank you!


[lejianwen/rustdesk-server]: https://github.com/lejianwen/rustdesk-server
