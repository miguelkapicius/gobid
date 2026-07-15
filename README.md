# GoBid

Backend de um sistema de leilões (auction) em tempo real, escrito em Go. Usuários podem cadastrar produtos para leilão e dar lances via WebSocket enquanto o leilão estiver ativo.

## Stack

- **Go** 1.26
- **chi** — roteador HTTP
- **pgx/v5** + **PostgreSQL** — acesso a dados
- **sqlc** — geração de código a partir de queries SQL
- **tern** — migrations de banco de dados
- **scs** — gerenciamento de sessão (armazenada no Postgres via `pgxstore`)
- **gorilla/websocket** — comunicação em tempo real dos leilões
- **gorilla/csrf** — proteção CSRF (atualmente desabilitada no `routes.go`)
- **air** — live reload em desenvolvimento

## Arquitetura

```
cmd/
  api/            # entrypoint da API HTTP
  terndotenv/     # executa as migrations do tern carregando o .env
internal/
  api/            # handlers HTTP, middlewares e definição de rotas
  services/       # regras de negócio (users, products, bids, auction rooms)
  usecase/        # request/response DTOs e validação de cada caso de uso
  store/pgstore/  # código gerado pelo sqlc + migrations e queries SQL
  validator/      # helpers de validação
  jsonutils/      # (de)serialização e resposta JSON padronizada
```

Cada leilão ativo roda em uma goroutine própria (`AuctionRoom`), registrada em um `AuctionLobby` compartilhado. Clientes conectados via WebSocket enviam e recebem lances em tempo real através dessa room.

## Pré-requisitos

- Go 1.26+
- Docker (para subir o PostgreSQL) ou uma instância própria do Postgres
- [tern](https://github.com/jackc/tern) para rodar as migrations (instalado como tool do módulo)

## Configuração

Crie um arquivo `.env` na raiz do projeto com as seguintes variáveis:

```env
GOBID_DATABASE_HOST=localhost
GOBID_DATABASE_PORT=5432
GOBID_DATABASE_USER=postgres
GOBID_DATABASE_PASSWORD=postgres
GOBID_DATABASE_NAME=gobid
GOBID_CSRF_KEY=uma-chave-secreta
```

## Como rodar

1. Subir o banco de dados:

   ```sh
   docker compose up -d
   ```

2. Rodar as migrations:

   ```sh
   go run ./cmd/terndotenv
   ```

3. Iniciar a API:

   ```sh
   go run ./cmd/api
   ```

   ou com live reload, usando [air](https://github.com/air-verse/air):

   ```sh
   go tool air
   ```

O servidor sobe em `http://localhost:3080`.

## Endpoints

| Método | Rota | Autenticação | Descrição |
| --- | --- | --- | --- |
| POST | `/api/v1/users/signup` | não | Cria um novo usuário |
| POST | `/api/v1/users/login` | não | Autentica e inicia sessão |
| POST | `/api/v1/users/logout` | sim | Encerra a sessão |
| POST | `/api/v1/products` | sim | Cria um produto e abre o leilão |
| GET | `/api/v1/products/ws/subscribe/{product_id}` | sim | Conecta via WebSocket ao leilão do produto e envia/recebe lances |

Autenticação é feita por sessão (cookie), definida no login e verificada pelo `AuthMiddleware`.

## Banco de dados

Tabelas principais (ver `internal/store/pgstore/migrations`):

- `users` — usuários (username, email, senha com hash, bio)
- `sessions` — sessões HTTP geridas pelo `scs`
- `products` — produtos leiloados (preço base, término do leilão, vendido/não vendido)
- `bids` — lances feitos por usuários em produtos

As queries usadas pela aplicação ficam em `internal/store/pgstore/queries` e o código de acesso a dados é gerado via `sqlc` (`internal/store/pgstore/sqlc.yaml`).
