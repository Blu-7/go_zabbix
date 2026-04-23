# Zabbix + PostgreSQL + Go tenant provisioner

Stack này chạy Zabbix với PostgreSQL và một provisioner viết bằng Go để:
1. gọi endpoint tenant (`get-tenant.php`) với API key trong JSON body;
2. đồng bộ tenant vào Zabbix (host, web scenario, trigger);
3. disable tenant không còn active.

## Thành phần

- `docker/zabbix-server/Dockerfile`: image Zabbix Server.
- `docker/zabbix-web/Dockerfile`: image Zabbix Web.
- `docker/provisioner-go/Dockerfile`: image Go provisioner.
- `docker-compose.yml`: `postgres`, `zabbix-server`, `zabbix-web`, `tenant-provisioner-go`.
- `.env.example`: biến môi trường mẫu.

## Cách chạy

1. Tạo file môi trường:

   ```bash
   cp .env.example .env
   ```

2. Bắt buộc cập nhật key xác thực tenant endpoint:

   ```bash
   TENANT_API_KEY=...
   ```

3. Khởi động:

   ```bash
   docker compose up -d --build
   ```

4. Truy cập UI:
   - URL: `http://localhost:8080` (hoặc cổng `ZBX_WEB_PORT`)
   - User mặc định: `Admin`
   - Password mặc định: `zabbix`

## Điểm quan trọng

- Provisioner luôn gọi `TENANT_API_URL` bằng `POST` JSON:

  ```json
  {"password":"TENANT_API_KEY"}
  ```

- Nếu `TENANT_API_KEY` rỗng, service sẽ fail ngay khi khởi động.

## Dừng hệ thống

```bash
docker compose down
```
