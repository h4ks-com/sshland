# SSHLAND

Source code for the menus and ssh apps under:
```
ssh h4ks.com
```

## Installation & development

```bash
cp .env.example .env
```

Modify what you need or just use env variables instead.

```bash
go mod download
go run main.go
```

That will run the menu. To run the whole stack with the apps you better off using docker-compose.

```bash
docker compose up
```
