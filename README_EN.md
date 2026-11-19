# RustDesk API

This project implements the RustDesk API using Go, and includes both a web UI and web client. RustDesk is a remote
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
> [lejianwen/rustdesk-server] is a fork of the official RustDesk Server repository.
> 1. Solves the API connection timeout issue.
> 2. Can enforce login before initiating a connection.
> 3. Supports client websocket.


# Features

- PC API
    - Personal API
    - Login
    - Address Book
    - Groups
    - Authorized login, 
      - supports `GitHub`, `Google` and `OIDC` login, 
      - supports `web admin` authorized login, 
      - supports LDAP(test AD and openladp) if API Server config
    - i18n
- Web Admin
    - User Management
    - Device Management
    - Address Book Management
    - Tag Management
    - Group Management
    - OAuth Management
    - LDAP Config by config file or ENV
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
Basic implementation of the PC client's primary interfaces.Supports the Personal version api, which can be enabled by configuring the `rustdesk.personal` file or the `RUSTDESK_API_RUSTDESK_PERSONAL` environment variable.

<table>
    <tr>
      <td width="50%" align="center" colspan="2"><b>Login</b></td>
    </tr>
    <tr>
        <td width="50%" align="center" colspan="2"><img src="docs/en_img/pc_login.png"></td>
    </tr>
     <tr>
      <td width="50%" align="center"><b>Address Book</b></td>
      <td width="50%" align="center"><b>Groups</b></td>
    </tr>
    <tr>
        <td width="50%" align="center"><img src="docs/en_img/pc_ab.png"></td>
        <td width="50%" align="center"><img src="docs/en_img/pc_gr.png"></td>
    </tr>
</table>

### Web Admin

* The frontend and backend are separated to provide a user-friendly management interface, primarily for managing and
displaying data. Build the console from the companion Web repository. [rustdesk-web](https://github.com/atishoo/rustdesk-web)

* Admin panel URL: `http://<your server[:port]>/_admin/`
* For the initial installation, the admin username is `admin`, and the password will be printed in the console. You can change the password via the [command line](#CLI).

  ![img.png](./docs/init_admin_pwd.png)


1. Admin interface:
   ![web_admin](docs/en_img/web_admin.png)
2. Regular user interface:
   ![web_user](docs/en_img/web_admin_user.png)

3. Each user can have multiple address books, which can also be shared with other users.
4. Groups can be customized for easy management. Currently, two types are supported: `shared group` and `regular group`.
5. You can directly launch the client or open the web client for convenience; you can also share it with guests, who can remotely access the device via the web client.
6. OAuth support: Currently, `GitHub`, `Google` and `OIDC`  are supported. You need to create an `OAuth App` and configure it in
   the admin panel.
    - For `Google` and `Github`, you don't need to fill the `Issuer` and `Scpoes`
    - For `OIDC`, you must set the `Issuer`. And `Scopes` is optional which default is `openid,email,profile`, please make sure this `Oauth App` can access `sub`, `email` and `preferred_username`
    - Create a `GitHub OAuth App`
      at `Settings` -> `Developer settings` -> `OAuth Apps` -> `New OAuth App` [here](https://github.com/settings/developers).
    - Set the `Authorization callback URL` to `http://<your server[:port]>/api/oidc/callback`,
      e.g., `http://127.0.0.1:21114/api/oidc/callback`.
   
7. Login logs
8. Connection logs
9. File transfer logs
10. Server control
  - `Simple mode`, some simple commands have been GUI-ized and can be executed directly in the backend
    ![rustdesk_command_simple](./docs/en_img/rustdesk_command_simple.png)

  - `Advanced mode`, commands can be executed directly in the backend
    * Official commands can be used
    * Custom commands can be added
    * Custom commands can be executed

11. **LDAP support**. Once LDAP is configured on the API server, users can sign in with directory accounts (tested with AD and OpenLDAP). Before enabling `ldap.user.sync`, keep an enabled local administrator outside LDAP synchronization and verify that it can sign in independently. Synchronization applies directory administrator role removals even when only one administrator remains. If the directory verifies the credentials but the role lookup or synchronization fails, the sign-in is denied.
  
### Web Client:

1. If you're already logged into the admin panel, the web client will log in automatically.
2. If you're not logged in, simply click the login button in the top right corner, and the API server will be
   pre-configured.
3. After logging in, the ID server and key will be automatically synced.
4. The address book will also be automatically saved to the web client for convenient use.

The built-in `/webclient/` uses its own origin for account login and configuration. On a reverse proxy, forward `/webclient/`, `/webclient-config/`, and `/api/` under the same domain.

Share links redeem through the Web Client origin at `/api/shared-peer` by default. If a standalone Web Client does not proxy that route, set `rustdesk.webclient-share-api-server` or `RUSTDESK_API_RUSTDESK_WEBCLIENT_SHARE_API_SERVER` to the API base URL, such as `https://api.example.com/rustdesk`. Serve or proxy `/webclient-config/index.js` to load the share redemption settings from that API. For a cross-origin deployment, allow the standalone Web Client origin on `/api/shared-peer` at your API gateway.

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

Password login through Vite's default same-origin proxy works with an empty origin allowlist. To call the API across origins during local development or use WebAuth or OIDC from Vite, explicitly list the development origin, such as `http://127.0.0.1:5174`, in the local API's `app.admin-origins` setting or `RUSTDESK_API_APP_ADMIN_ORIGINS` environment variable.

Admin OAuth requests require an absolute `redirect_url`. Its origin must match the request origin and appear in `app.admin-origins` or match `rustdesk.api-server`. For same-origin reverse proxy deployments, preserve the original `Host`, set `X-Forwarded-Proto` to the public scheme, and list the immediate proxy address in `gin.trust-proxy` or `RUSTDESK_API_GIN_TRUST_PROXY`.

Anonymous Web Client sharing requires `rustdesk.id-server`. For a public hostname, a proxy, or an external ID Server, set `rustdesk.must-login` or `RUSTDESK_API_RUSTDESK_MUST_LOGIN` to `true` or `false` to match the target's policy. A policy that requires login or cannot be confirmed blocks creating and redeeming anonymous shares. You can omit this setting to query the local ID Server when `rustdesk.id-server` uses `127.0.0.1` or `::1` and its port matches `admin.id-server-port`.


### Environment Variables
The environment variables correspond one-to-one with the configurations in the `conf/config.yaml` file. The prefix for variable names is `RUSTDESK_API`.
The table below does not list all configurations. Please refer to the configurations in `conf/config.yaml`.

| Variable Name                                          | Description                                                                                                                                         | Example                       |
|--------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------|
| TZ                                                     | timezone                                                                                                                                            | Asia/Shanghai                 |
| RUSTDESK_API_LANG                                      | Language                                                                                                                                            | `en`,`zh-CN`                  |
| RUSTDESK_API_APP_WEB_CLIENT                            | web client on/off; 1: on, 0 off, default: 1                                                                                                         | 1                             |
| RUSTDESK_API_APP_ADMIN_ORIGINS                         | Comma-separated trusted console origins. The list is empty by default. | `http://127.0.0.1:5174` |
| RUSTDESK_API_APP_REGISTER                              | register enable; `true`, `false`; default:`false`                                                                                                   | `false`                       |
| RUSTDESK_API_APP_SHOW_SWAGGER                          | swagger visible; 1: yes, 0: no; default: 0                                                                                                          | `0`                           |
| RUSTDESK_API_APP_TOKEN_EXPIRE                          | token expire duration                                                                                                                               | `168h`                        |
| RUSTDESK_API_APP_DISABLE_PWD_LOGIN                     | disable password login                                                                                                                              | `false`                       |
| RUSTDESK_API_APP_REGISTER_STATUS                       | register user default status ; 1 enabled , 2 disabled ; default 1                                                                                   | `1`                           |
| RUSTDESK_API_APP_CAPTCHA_THRESHOLD                     | Use -1 to disable CAPTCHA or 0 to always require it. A positive threshold must be lower than the ban threshold when both are enabled. | `-1` |
| RUSTDESK_API_APP_BAN_THRESHOLD                         | Block password login and related requests for 30 minutes after this many failures from the same IP within 10 minutes. Use 0 to disable the ban. | `10` |
| ----- ADMIN Configuration-----                         | ----------                                                                                                                                          | ----------                    |
| RUSTDESK_API_ADMIN_TITLE                               | Admin Title                                                                                                                                         | `RustDesk Api Admin`          |
| RUSTDESK_API_ADMIN_HELLO                               | Admin welcome message, you can use `html`                                                                                                           |                               |
| RUSTDESK_API_ADMIN_HELLO_FILE                          | Admin welcome message file,<br>will override `RUSTDESK_API_ADMIN_HELLO`                                                                             | `./conf/admin/hello.html`     |
| ----- GIN Configuration -----                          | ---------------------------------------                                                                                                             | ----------------------------- |
| RUSTDESK_API_GIN_TRUST_PROXY                           | Trusted proxy IPs, separated by commas.                                                                                                             | 192.168.1.2,192.168.1.3       |
| ----- GORM Configuration -----                         | ---------------------------------------                                                                                                             | ----------------------------- |
| RUSTDESK_API_GORM_TYPE                                 | Database type (`sqlite` or `mysql`). Default is `sqlite`.                                                                                           | sqlite                        |
| RUSTDESK_API_GORM_MAX_IDLE_CONNS                       | Maximum idle connections                                                                                                                            | 10                            |
| RUSTDESK_API_GORM_MAX_OPEN_CONNS                       | Maximum open connections                                                                                                                            | 100                           |
| RUSTDESK_API_RUSTDESK_PERSONAL                         | Open Personal Api 1:Enable,0:Disable                                                                                                                | 1                             |
| ----- MYSQL Configuration -----                        | ---------------------------------------                                                                                                             | ----------------------------- |
| RUSTDESK_API_MYSQL_USERNAME                            | MySQL username                                                                                                                                      | root                          |
| RUSTDESK_API_MYSQL_PASSWORD                            | MySQL password                                                                                                                                      | 111111                        |
| RUSTDESK_API_MYSQL_ADDR                                | MySQL address                                                                                                                                       | 192.168.1.66:3306             |
| RUSTDESK_API_MYSQL_DBNAME                              | MySQL database name                                                                                                                                 | rustdesk                      |
| RUSTDESK_API_MYSQL_TLS                             | Whether to enable TLS, optional values: `true`, `false`, `skip-verify`, `custom` | `false`                       |
| ----- RUSTDESK Configuration -----                     | ---------------------------------------                                                                                                             | ----------------------------- |
| RUSTDESK_API_RUSTDESK_ID_SERVER                        | Rustdesk ID server address                                                                                                                          | 192.168.1.66:21116            |
| RUSTDESK_API_RUSTDESK_RELAY_SERVER                     | Rustdesk relay server address                                                                                                                       | 192.168.1.66:21117            |
| RUSTDESK_API_RUSTDESK_API_SERVER                       | Rustdesk API server address                                                                                                                         | http://192.168.1.66:21114     |
| RUSTDESK_API_RUSTDESK_KEY                              | Rustdesk key                                                                                                                                        | 123456789                     |
| RUSTDESK_API_RUSTDESK_KEY_FILE                         | Rustdesk key file                                                                                                                                   | `./conf/data/id_ed25519.pub`  |
| RUSTDESK_API_RUSTDESK<br/>_WEBCLIENT_MAGIC_QUERYONLINE | New online query method is enabled in the web client v2; '1': Enabled, '0': Disabled, not enabled by default                                        | `0`                           |
| RUSTDESK_API_RUSTDESK_WS_HOST                          | Custom Websocket Host                                                                                                                               | `wss://192.168.1.123:1234`    |
| ---- PROXY -----                                       | ---------------                                                                                                                                     | ----------                    |
| RUSTDESK_API_PROXY_ENABLE                              | proxy_enable :`false`, `true`                                                                                                                       | `false`                       |
| RUSTDESK_API_PROXY_HOST                                | proxy_host                                                                                                                                          | `http://127.0.0.1:1080`       |
| ----JWT----                                            | --------                                                                                                                                            | --------                      |
| RUSTDESK_API_JWT_KEY                                   | Custom JWT KEY, if empty JWT is not enabled.<br/>If `MUST_LOGIN` from `lejianwen/rustdesk-server` is not used, it is recommended to leave it empty. |                               |
| RUSTDESK_API_JWT_EXPIRE_DURATION                       | JWT expire duration                                                                                                                                 | `168h`                        |

### Installation Steps

#### Running via Docker

1. Run directly with Docker. Configuration can be modified by mounting the config file `/app/conf/config.yaml`, or by
   using environment variables to override settings.
    
    ```bash
    docker run -d --name rustdesk-api -p 21114:21114 \
    -v /data/rustdesk/api:/app/data \
    -e RUSTDESK_API_LANG=en \
    -e RUSTDESK_API_RUSTDESK_ID_SERVER=192.168.1.66:21116 \
    -e RUSTDESK_API_RUSTDESK_RELAY_SERVER=192.168.1.66:21117 \
    -e RUSTDESK_API_RUSTDESK_API_SERVER=http://192.168.1.66:21114 \
    -e RUSTDESK_API_RUSTDESK_KEY=abc123456 \
    lejianwen/rustdesk-api
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

3. Build the sibling console checkout with Node.js 22. [rustdesk-web](https://github.com/atishoo/rustdesk-web)
   ```bash
   (cd ../rustdesk-web && npm ci && npm run build)
   mkdir -p resources/admin
   cp -a ../rustdesk-web/dist/. resources/admin/
   ```
   GitHub Actions builds `rustdesk-web/main` under the same owner by default. Use the repository variables `WEBCLIENT_REPOSITORY` and `WEBCLIENT_REF` to select a matching console revision. For a private console repository, set `RUSTDESK_WEB_READ_TOKEN` in the API repository with read access to the console contents. Publish the matching console revision before releasing the API.

   To build the development image locally, run this from the API repository root.
   ```bash
   docker build -f Dockerfile.dev --build-context rustdesk-web=../rustdesk-web .
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
   > `RUSTDESK_API_GIN_RESOURCES_PATH` environment variable. Example:
   > ```bash
   > RUSTDESK_API_GIN_RESOURCES_PATH=/opt/rustdesk-api/resources ./apimain -c /opt/rustdesk-api/conf/config.yaml
   > ```

5. To compile, change to the project root directory. For Windows, run `build.bat`, and for Linux, run `build.sh`. After
   compiling, the corresponding executables will be generated in the `release` directory. Run the compiled executables
   directly.

6. Open your browser and visit `http://<your server[:port]>/_admin/`, with default credentials `admin admin`. Please
   change the password promptly.

#### Running the UltraPro API + Web image

After the UltraPro branch build completes, deploy the multi-architecture image `ghcr.io/atishoo/rustdesk-api:ultrapro`. It contains the API and management console. Run ID Server and Relay Server separately with `ghcr.io/atishoo/rustdesk-server:ultrapro`. For private GHCR packages, run `docker login ghcr.io` before starting the existing Compose deployment.

#### Running with my forked server-s6 image

- Connection timeout issue resolved
- Can enforce login before initiating a connection
- github https://github.com/lejianwen/rustdesk-server

```yaml
 networks:
   rustdesk-net:
     external: false
 services:
   rustdesk:
     ports:
       - 21114:21114
       - 21115:21115
       - 21116:21116
       - 21116:21116/udp
       - 21117:21117
       - 21118:21118
       - 21119:21119
     image: lejianwen/rustdesk-server-s6:latest
     environment:
       - RELAY=<relay_server[:port]>
       - ENCRYPTED_ONLY=1
       - MUST_LOGIN=N
       - TZ=Asia/Shanghai
       - RUSTDESK_API_RUSTDESK_ID_SERVER=<id_server[:21116]>
       - RUSTDESK_API_RUSTDESK_RELAY_SERVER=<relay_server[:21117]>
       - RUSTDESK_API_RUSTDESK_API_SERVER=http://<api_server[:21114]>
       - RUSTDESK_API_KEY_FILE=/data/id_ed25519.pub
       - RUSTDESK_API_JWT_KEY=xxxxxx # jwt key
     volumes:
       - /data/rustdesk/server:/data
       - /data/rustdesk/api:/app/data #将数据库挂载
     networks:
       - rustdesk-net
     restart: unless-stopped
       
```
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
