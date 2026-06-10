# GoGap

GoGap 是一个基金折溢价查看器。

当前项目版本：`v1.0.0`。

数据仅供参考：`数据来源于公开信息；交易时段折溢价可基于盘中估值与最新场内价格计算，非交易时段基于官方已披露净值；不构成投资建议；投资有风险。`

## 功能

- 查看基金官方净值、交易时段盘中估值、场内价格、折溢价率和成交额。
- 自动发现数据源返回的基金。
- 支持自选、搜索、筛选、排序等。

## 环境要求

- Go 1.26.4+
- Node.js 22+
- pnpm 11+

## 配置

程序启动时会先读取运行目录下的 `.env`，再读取同名环境变量，最后读取命令行参数。推荐在项目根目录放一个 `.env`，也可以先复制 `.env.example`。

支持的配置变量如下：

- `GOGAP_ADDR`：监听地址，默认 `127.0.0.1:8080`
- `GOGAP_DB`：SQLite 数据文件路径，默认 `data/gogap.db`
- `GOGAP_LOG`：日志文件路径，默认空，表示输出到标准错误
- `GOGAP_POLL_INTERVAL`：行情刷新间隔，默认 `120s`
- `GOGAP_DEV`：开发模式开关，默认 `false`
- `GOGAP_SOURCES`：数据源客户端名称列表，当前支持 `eastmoney`

可选择的数据源客户端：

- `eastmoney`：东方财富公开接口适配器，提供场内价格、官方已披露净值和交易时段盘中估值

## 文件结构

```text
.
├── cmd/gogap/                 # 程序入口，读取配置、组装 API/SSE/scheduler/source/store
├── internal/
│   ├── api/                   # HTTP 路由、REST API、页面模板
│   │   └── templates/         # 后端渲染用的静态 HTML 片段
│   ├── config/                # .env、环境变量和命令行参数解析
│   ├── domain/                # 基金分类、快照、折溢价等核心数据结构
│   ├── scheduler/             # 启动拉取、周期刷新、快照合成和 SSE 推送触发
│   ├── source/                # 数据源接口、多数据源组合和基金池定义
│   │   └── eastmoney/         # 东方财富公开接口适配器、解析器、动态基金发现
│   ├── sse/                   # Server-Sent Events 广播 hub
│   └── store/                 # SQLite 自选列表和缓存存储
├── web/                       # 前端源码和构建配置
│   ├── src/                   # 原生 TypeScript UI、状态、筛选、渲染和 API 调用
│   ├── index.html             # 前端开发入口模板
│   └── package.json           # 前端依赖和构建脚本
├── .env.example               # 本地运行配置示例
├── AGENT.md                   # AI 开发代理项目规则
├── README.md                  # 项目说明和部署文档
├── go.mod / go.sum            # Go 模块依赖
└── web/embed.go               # 将 web/dist 嵌入 Go 二进制
```

## 部署

### 本地构建

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go mod download
go build -o gogap ./cmd/gogap
```

运行

```sh
./gogap --addr 127.0.0.1:8080 --db data/gogap.db
```

### Docker 部署

使用 Docker：

```sh
docker container run -p 8080:8080 -v ./gogap-data:/app/data ghcr.io/planet756/gogap:latest
```

使用 Docker Compose：

```sh
docker compose up -d
```

最后打开：

```text
http://127.0.0.1:8080
```

## 注意事项

- 仅建议在本地或受控内网使用。
- 数据来源于公开信息。
- 交易时段折溢价可基于盘中估值与最新场内价格计算，非交易时段基于官方已披露净值。
- 不构成投资建议；投资有风险。
