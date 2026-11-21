# 卡皮远控 API

[English Doc](README_EN.md)

本项目使用 Go 实现了卡皮远控的 API，并包含了 Web Admin 和 Web 客户端。


<div align=center>
<img src="https://img.shields.io/badge/golang-1.22-blue"/>
<img src="https://img.shields.io/badge/gin-v1.9.0-lightBlue"/>
<img src="https://img.shields.io/badge/gorm-v1.25.7-green"/>
<img src="https://img.shields.io/badge/swag-v1.16.3-yellow"/>
<img src="https://goreportcard.com/badge/github.com/lejianwen/rustdesk-api/v2"/>
<img src="https://github.com/lejianwen/rustdesk-api/actions/workflows/build.yml/badge.svg"/>
</div>

## 搭配[lejianwen/rustdesk-server]使用更佳。
> [lejianwen/rustdesk-server]fork自 RustDesk Server 上游仓库
> 1. 解决了使用API链接超时问题
> 2. 可以强制登录后才能发起链接
> 3. 支持客户端websocket



# 特性

- PC端API
    - 个人版API
    - 登录
    - 地址簿
    - 群组
    - 授权登录
      - 支持`github`, `google` 和 `OIDC` 登录，
      - 支持`web后台`授权登录
      - 支持`LDAP`(AD和OpenLDAP已测试), 如果API Server配置了LDAP
    - i18n
- Web Admin
    - 用户管理
    - 设备管理
    - 地址簿管理
    - 标签管理
    - 群组管理
    - Oauth 管理
    - 配置LDAP, 配置文件或者环境变量
    - 登录日志
    - 链接日志
    - 文件传输日志
    - 快速使用web client
    - i18n
    - 通过 web client 分享给游客
    - server控制(一些官方的简单的指令 [WIKI](https://github.com/lejianwen/rustdesk-api/wiki/Rustdesk-Command))
- Web Client
    - 自动获取API server
    - 自动获取ID服务器和KEY
    - 自动获取地址簿
    - 游客通过临时分享链接直接远程到设备
- CLI
    - 重置管理员密码

## 功能


### API 服务 
基本实现了PC端基础的接口。支持Personal版本接口，可以通过配置文件`capylink.personal`或环境变量`CAPYLINK_API_CAPYLINK_PERSONAL`来控制是否启用

配置本部署的 API 地址后，在客户端账户页面登录；登录后可使用地址簿和设备组访问已授权的设备。

### 同账号设备免输密码

在 `capylink.device-credential-key` 配置独立的 32 字节加密密钥（标准 Base64 编码），或设置 `CAPYLINK_API_CAPYLINK_DEVICE_CREDENTIAL_KEY`。例如使用 `openssl rand -base64 32` 生成。留空时关闭此功能，格式错误时 API 拒绝启动。

使用本仓库的 Compose 部署时，仅更新镜像仍默认关闭免输密码。按以下步骤启用：

1. 执行一次 `openssl rand -base64 32`，将结果保存到 Compose 同目录的 `.env`：`CAPYLINK_API_CAPYLINK_DEVICE_CREDENTIAL_KEY=生成的密钥`。后续部署继续使用此值。
2. 两端原生 App 都更新到包含同账号免密功能的版本，配置同一部署的 API 地址（支持 HTTP 和 HTTPS）和正确的 ID Server 公钥。
3. 执行 `docker compose up -d --force-recreate capylink-api`，重建 API 容器以应用配置。
4. 两端 App 登录同一账号；升级前已登录的 App 先退出账号再重新登录。保持被控端 App 运行，等待下一次凭据刷新（最多约 30 秒）后连接。仅在 Web 控制台登录不能代替 App 登录。

被控端目前支持新版 Flutter 桌面 App 和 Android App；旧版 Sciter 客户端（包括 Windows 32 位版本）不会上传凭据。账号能登录不代表已启用设备免输密码。

两端原生 App 须指向同一部署，并配置正确的 ID Server 公钥。登录同一账号、设备 ID 与 UUID 完整且已确认归属后，被控端为该账号生成独立随机连接凭据，API 使用 AES-256-GCM 加密后保存到上传设备的登录会话。它不读取或修改设备永久密码。连接时根据账号归属、完整设备身份、设备公钥和当前挑战盐取得凭据，继续遵循被控端的连接确认、权限和 2FA 设置。

App 每 30 秒刷新凭据；API 与被控端的授权租约均为 90 秒。退出账号时，被控端立即清除本地凭据，并尝试删除云端密文；GUI 进程退出、网络中断或账号失效导致无法续期时，凭据最多再有效 90 秒。退出或撤销上传会话、删除设备、修改设备 ID/UUID 后，API 不再提供该会话的凭据。已下发的副本无法远程销毁，控制端只在当前连接内存中使用，失效由被控端租约控制。

这些接口支持 HTTP 和 HTTPS。API 加密密钥只用于服务端保存密文；更换密钥后，在线 App 重新上传才会恢复免输密码。

### Web Admin:

* 管理控制台在配套的 Web 仓库中构建。[capylink-web](https://github.com/atishoo/rustdesk-web)

* 后台访问地址是`http://<your server>[:port]/_admin/`
* 初次安装不会预置管理员账号。开启注册后，首个完成邮箱验证码注册的账号会自动成为超级管理员。

1. 管理员可通过侧栏管理用户、设备、地址簿及服务配置。
2. 普通用户可查看个人设备、地址簿、分享记录和登录日志。

3. 每个用户可以多个地址簿，也可以将地址簿共享给其他用户
4. 分组可以自定义，方便管理，暂时支持两种类型: `共享组` 和 `普通组`
5. 可以直接打开webclient，方便使用；也可以分享给游客，游客可以直接通过webclient远程到设备
6. Oauth,支持了`Github`, `Google` 以及 `OIDC`, 需要创建一个`OAuth App`，然后配置到后台
    - 对于`Google` 和 `Github`, `Issuer` 和 `Scopes`不需要填写.
    - 对于`OIDC`, `Issuer`是必须的。`Scopes`是可选的，默认为 `openid,profile,email`. 确保可以获取 `sub`,`email` 和`preferred_username`
    - `github oauth app`在`Settings`->`Developer settings`->`OAuth Apps`->`New OAuth App`
      中创建,地址 [https://github.com/settings/developers](https://github.com/settings/developers)
    - `Authorization callback URL`填写`http://<your server[:port]>/api/oidc/callback`
      ，比如`http://127.0.0.1:21114/api/oidc/callback`
7. 登录日志
8. 链接日志
9. 文件传输日志
10. 服务配置与命令

    - **服务配置**：在侧栏打开“服务配置”，核对 API、ID Server、Relay Server 和控制台配置。
    - **简单命令**：打开“服务命令” → “简单”，通过表单读取或设置服务参数。
    - **高级命令**：打开“服务命令” → “高级”，选择目标服务、发送命令并管理自定义命令。

11. **LDAP 支持**。在 API Server 配置 LDAP 后，可以用目录账号登录（已在 AD 和 OpenLDAP 上验证）。启用 `ldap.user.sync` 前，保留一个不受 LDAP 同步影响的启用本地管理员，并验证其可独立登录。同步会应用目录管理员组的权限撤回，包括仅剩一个管理员的情况。LDAP 凭据验证通过后，如果组权限查询或同步失败，本次登录会被拒绝。

### Web Client:

1. 如果已经登录了后台，web client将自动直接登录
2. 如果没登录后台，点击右上角登录即可，api server已经自动配置好了
3. 登录后，会自动同步ID服务器和KEY
4. 登录后，会将地址簿自动保存到web client中，方便使用

内置 `/webclient/` 通过页面同源访问登录和账号配置。反向代理部署时，在同一域名转发 `/webclient/`、`/webclient-config/` 和 `/api/`。

分享链接默认在 Web Client 同源访问 `/api/shared-peer`。独立部署且未转发该路由时，设置 `capylink.webclient-share-api-server` 或 `CAPYLINK_API_CAPYLINK_WEBCLIENT_SHARE_API_SERVER` 为实际 API 基础地址，例如 `https://api.example.com/capylink`。独立 Web Client 还须提供或转发 `/webclient-config/index.js`，以加载该 API 的兑换配置。跨域部署需在 API 入口为该独立 Web Client 来源配置 `/api/shared-peer` 的 CORS。

### 自动化文档: 使用 Swag 生成 API 文档，方便开发者理解和使用 API。

1. 后台文档 `<youer server[:port]>/admin/swagger/index.html`
2. PC端文档 `<youer server[:port]>/swagger/index.html`
   ![api_swag](docs/api_swag.png)

### CLI

```bash
# 查看帮助
./apimain -h
```

#### 重置管理员密码
```bash
./apimain reset-admin-pwd <pwd>
```

## 安装与运行

### 相关配置

* [配置文件](./conf/config.yaml)
* 参考`conf/config.yaml`配置文件，修改相关配置。
* 如果`gorm.type`是`sqlite`，则不需要配置mysql相关配置。
* 语言如果不设置默认为`zh-CN`

Vite 默认通过同源代理访问 API，使用密码登录无需配置来源白名单。本地控制台跨域调用 API，或使用 Vite 地址进行 WebAuth 和 OIDC 登录时，在本机 API 的 `app.admin-origins` 或 `CAPYLINK_API_APP_ADMIN_ORIGINS` 中显式添加实际开发地址，例如 `http://127.0.0.1:5174`。

管理端 OAuth 请求须携带绝对 `redirect_url`，其来源须与请求来源一致，并已在 `app.admin-origins` 或 `capylink.api-server` 中配置。同源反向代理需保留原始 `Host`，将 `X-Forwarded-Proto` 设为公网协议，并在 `gin.trust-proxy` 或 `CAPYLINK_API_GIN_TRUST_PROXY` 中登记 API 的直接代理地址。

匿名 Web Client 分享需要配置 `capylink.id-server`。使用公网域名、代理或外部 ID Server 时，请按实际目标的策略将 `capylink.must-login` 或 `CAPYLINK_API_CAPYLINK_MUST_LOGIN` 设为 `true` 或 `false`。`true` 或无法确认的策略均会阻止创建和兑换匿名分享。只有 `capylink.id-server` 为 `127.0.0.1` 或 `::1` 且端口与 `admin.id-server-port` 一致时，才能省略策略并查询本机实例。

### 环境变量
环境变量和配置文件`conf/config.yaml`中的配置一一对应，变量名前缀是`CAPYLINK_API`，服务配置段使用 `CAPYLINK_API_CAPYLINK_*`。SQLite 数据库统一使用 `data/capylinkapi.db`。
下面表格并未全部列出，可以参考`conf/config.yaml`中的配置。

| 变量名                                                    | 说明                                                                             | 示例                           |
|--------------------------------------------------------|--------------------------------------------------------------------------------|------------------------------|
| TZ                                                     | 时区                                                                             | Asia/Shanghai                |
| CAPYLINK_API_LANG                                      | 语言                                                                             | `en`,`zh-CN`                 |
| CAPYLINK_API_APP_WEB_CLIENT                            | 是否启用web-client; 1:启用,0:不启用; 默认启用                                               | 1                            |
| CAPYLINK_API_APP_ADMIN_ORIGINS                         | 可信管理控制台 Origin，多个用英文逗号分隔，默认为空 | `http://127.0.0.1:5174` |
| CAPYLINK_API_APP_SHOW_SWAGGER                          | 是否可见swagger文档;`1`显示，`0`不显示，默认`0`不显示                                            | `1`                          |
| CAPYLINK_API_APP_TOKEN_EXPIRE                          | token有效时长                                                                      | `168h`                       |
| CAPYLINK_API_APP_CAPTCHA_THRESHOLD                     | 验证码触发次数，-1 不启用，0 一直启用。正数表示失败次数阈值，同时启用封禁时必须小于封禁阈值 | `-1` |
| CAPYLINK_API_APP_BAN_THRESHOLD                         | 同一 IP 在 10 分钟内登录失败达到阈值后，限制其密码登录及相关选项和验证码请求 30 分钟。已建立的会话与设备心跳可继续使用，0 表示关闭 | `10` |
| CAPYLINK_API_SMTP_HOST                                 | SMTP 服务器地址                                                                  | `smtp.aliyun.com`           |
| CAPYLINK_API_SMTP_USER                                 | SMTP 用户名                                                                      | `easyvcode@aliyun.com`      |
| CAPYLINK_API_SMTP_PASSWORD                             | SMTP 密码                                                                        |                               |
| CAPYLINK_API_SMTP_PORT                                 | SMTP 端口                                                                        | `465`                       |
| CAPYLINK_API_SMTP_CRYPTO                               | SMTP 加密方式，465 端口使用 `ssl`                                                | `ssl`                       |
| -----ADMIN配置-----                                      | ----------                                                                     | ----------                   |
| CAPYLINK_API_ADMIN_TITLE                               | 后台标题                                                                           | `capylink Api Admin`         |
| CAPYLINK_API_ADMIN_HELLO                               | 后台欢迎语，可以使用`html`                                                               |                              |
| CAPYLINK_API_ADMIN_HELLO_FILE                          | 后台欢迎语文件，如果内容多，使用文件更方便。<br>会覆盖`CAPYLINK_API_ADMIN_HELLO`                        | `./conf/admin/hello.html`    |
| -----GIN配置-----                                        | ----------                                                                     | ----------                   |
| CAPYLINK_API_GIN_TRUST_PROXY                           | 信任的代理 IP 列表，以 `,` 分隔，默认为空。反向代理部署时填入实际代理地址，登录保护才能按客户端 IP 分别计数 | 192.168.1.2,192.168.1.3 |
| -----GORM配置-----                                       | ----------                                                                     | ---------------------------  |
| CAPYLINK_API_GORM_TYPE                                 | 数据库类型sqlite或者mysql，默认sqlite                                                    | sqlite                       |
| CAPYLINK_API_GORM_MAX_IDLE_CONNS                       | 数据库最大空闲连接数                                                                     | 10                           |
| CAPYLINK_API_GORM_MAX_OPEN_CONNS                       | 数据库最大打开连接数                                                                     | 100                          |
| CAPYLINK_API_CAPYLINK_PERSONAL                         | 是否启用个人版API， 1:启用,0:不启用； 默认启用                                                   | 1                            |
| -----MYSQL配置-----                                      | ----------                                                                     | ----------                   |
| CAPYLINK_API_MYSQL_USERNAME                            | mysql用户名                                                                       | root                         |
| CAPYLINK_API_MYSQL_PASSWORD                            | mysql密码                                                                        | 111111                       |
| CAPYLINK_API_MYSQL_ADDR                                | mysql地址                                                                        | 192.168.1.66:3306            |
| CAPYLINK_API_MYSQL_DBNAME                              | mysql数据库名                                                                      | capylink                     |
| CAPYLINK_API_MYSQL_TLS                             | 是否启用TLS, 可选值: `true`, `false`, `skip-verify`, `custom` | `false`                      |
| -----卡皮远控配置-----                                   | ----------                                                                     | ----------                   |
| CAPYLINK_API_CAPYLINK_ID_SERVER                        | 卡皮远控的id服务器地址                                                               | 192.168.1.66:21116           |
| CAPYLINK_API_CAPYLINK_RELAY_SERVER                     | 卡皮远控的relay服务器地址                                                            | 192.168.1.66:21117           |
| CAPYLINK_API_CAPYLINK_API_SERVER                       | 卡皮远控的api服务器地址                                                              | http://192.168.1.66:21114    |
| CAPYLINK_API_CAPYLINK_KEY                              | 卡皮远控的key                                                                   | 123456789                    |
| CAPYLINK_API_CAPYLINK_KEY_FILE                         | 卡皮远控存放key的文件                                                               | `./conf/data/id_ed25519.pub` |
| CAPYLINK_API_CAPYLINK_WEBCLIENT<br/>_MAGIC_QUERYONLINE | Web client v2 中是否启用新的在线状态查询方法; `1`:启用,`0`:不启用,默认不启用                            | `0`                          |
| CAPYLINK_API_CAPYLINK_WS_HOST                          | 自定义Websocket Host                                                              | `wss://192.168.1.123:1234`   |
| ----PROXY配置-----                                       | ----------                                                                     | ----------                   |
| CAPYLINK_API_PROXY_ENABLE                              | 是否启用代理:`false`, `true`                                                         | `false`                      |
| CAPYLINK_API_PROXY_HOST                                | 代理地址                                                                           | `http://127.0.0.1:1080`      |
| ----JWT配置----                                          | --------                                                                       | --------                     |
| CAPYLINK_API_JWT_KEY                                   | 自定义JWT KEY,为空则不启用JWT<br/>如果没使用`lejianwen/rustdesk-server`中的`MUST_LOGIN`，建议设置为空 |                              |
| CAPYLINK_API_JWT_EXPIRE_DURATION                       | JWT有效时间                                                                        | `168h`                       |


### 运行

#### docker运行

1. 直接docker运行,配置可以通过挂载配置文件`/app/conf/config.yaml`来修改,或者通过环境变量覆盖配置文件中的配置

    ```bash
    docker run -d --name capylink-api -p 21114:21114 \
    -v /data/capylink/api:/app/data \
    -e TZ=Asia/Shanghai \
    -e CAPYLINK_API_LANG=zh-CN \
    -e CAPYLINK_API_CAPYLINK_ID_SERVER=192.168.1.66:21116 \
    -e CAPYLINK_API_CAPYLINK_RELAY_SERVER=192.168.1.66:21117 \
    -e CAPYLINK_API_CAPYLINK_API_SERVER=http://192.168.1.66:21114 \
    -e CAPYLINK_API_CAPYLINK_KEY=<key> \
    ghcr.io/atishoo/capylink-api:ultrapro
    ```

2. 使用`docker compose`，参考[WIKI](https://github.com/lejianwen/rustdesk-api/wiki)

#### 下载release直接运行

[下载地址](https://github.com/lejianwen/rustdesk-api/releases)

#### 源码安装

1. 克隆仓库
   ```bash
   git clone https://github.com/atishoo/rustdesk-api.git
   cd rustdesk-api
   ```

2. 安装依赖

    ```bash
    go mod tidy
    #安装swag，如果不需要生成文档，可以不安装
    go install github.com/swaggo/swag/cmd/swag@latest
    ```

3. 用 Node.js 22 编译同级目录的配套控制台。[capylink-web](https://github.com/atishoo/rustdesk-web)
   ```bash
   (cd ../rustdesk-web && npm ci && npm run build)
   mkdir -p resources/admin
   cp -a ../rustdesk-web/dist/. resources/admin/
   ```
   GitHub Actions 默认构建同一所有者的 `rustdesk-web`。可通过仓库变量 `WEBCLIENT_REPOSITORY` 和 `WEBCLIENT_REF` 指定配套仓库和版本。配套仓库为私有仓库时，在 API 仓库配置 `RUSTDESK_WEB_READ_TOKEN`，授予该 Web 仓库的内容读取权限。发布 API 前先发布对应的 Web 版本。

   如需本地构建开发镜像，在 API 仓库根目录运行以下命令。
   ```bash
   docker build -f Dockerfile.dev --build-context capylink-web=../rustdesk-web .
   ```
   定制客户端可以在 GitHub Actions 仓库变量、Compose 环境或 `docker build --build-arg` 中设置 `VITE_NATIVE_CLIENT_SCHEME`，值为已注册的客户端协议名。
4. 运行
    ```bash
    #直接运行
    go run cmd/apimain.go
    #或者使用generate_api.go生成api并运行
    go generate generate_api.go
    ```
   > 注意：使用 `go run` 或编译后的二进制时，当前目录下必须存在 `conf` 和 `resources`
   > 目录。如果在其他目录运行，可通过 `-c` 和环境变量
   > `CAPYLINK_API_GIN_RESOURCES_PATH` 指定绝对路径，例如：
   > ```bash
   > CAPYLINK_API_GIN_RESOURCES_PATH=/opt/capylink-api/resources ./apimain -c /opt/capylink-api/conf/config.yaml
   > ```
5. 编译，如果想自己编译,先cd到项目根目录，然后windows下直接运行`build.bat`,linux下运行`build.sh`,编译后会在`release`
   目录下生成对应的可执行文件。直接运行编译后的可执行文件即可。

6. 打开浏览器访问`http://<your server[:port]>/_admin/`，使用邮箱获取验证码完成注册。首个注册账号会自动成为超级管理员。


#### 使用 UltraPro API + Web 镜像

UltraPro 分支构建完成后，可直接部署多架构镜像 `ghcr.io/atishoo/capylink-api:ultrapro`。镜像包含 API 和管理控制台。ID Server 与 Relay Server 使用独立的 `ghcr.io/atishoo/capylink-server:ultrapro` 镜像。GHCR 包为私有时，先执行 `docker login ghcr.io`，再使用这两个镜像和现有 Compose 配置启动服务。

#### 使用 Compose 运行

使用本仓库的 `docker-compose.yaml` 启动 API、管理控制台和独立的 ID Server、Relay Server。环境变量名称见该文件及上方配置表。

## 其他

- [WIKI](https://github.com/lejianwen/rustdesk-api/wiki)
- [链接超时问题](https://github.com/lejianwen/rustdesk-api/issues/92)
- [修改客户端ID](https://github.com/abdullah-erturk/RustDesk-ID-Changer)
- [webclient来源](https://hub.docker.com/r/keyurbhole/flutter_web_desk)


## 鸣谢

感谢所有做过贡献的人!

<a href="https://github.com/lejianwen/rustdesk-api/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=lejianwen/rustdesk-api" />
</a>

## 感谢你的支持！如果这个项目对你有帮助，请点个⭐️鼓励一下，谢谢！

[lejianwen/rustdesk-server]: https://github.com/lejianwen/rustdesk-server
