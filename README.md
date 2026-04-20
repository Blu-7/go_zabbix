# Zabbix + PostgreSQL (Docker)

Repo này đã có sẵn bộ Dockerfile + docker compose để chạy Zabbix với PostgreSQL.

## Thành phần

- `docker/zabbix-server/Dockerfile`: image cho Zabbix Server (PostgreSQL).
- `docker/zabbix-web/Dockerfile`: image cho Zabbix Web (Nginx + PostgreSQL).
- `docker-compose.yml`: chạy `postgres`, `zabbix-server`, `zabbix-web`.
- `.env.example`: biến môi trường mẫu.

## Cách chạy

1. Tạo file môi trường:

   ```bash
   cp .env.example .env
   ```

2. Khởi động:

   ```bash
   docker compose up -d --build
   ```

3. Truy cập UI:

   - URL: `http://localhost:8080` (hoặc cổng trong `ZBX_WEB_PORT`)
   - User mặc định: `Admin`
   - Password mặc định: `zabbix`

## Dừng hệ thống

```bash
docker compose down
```
