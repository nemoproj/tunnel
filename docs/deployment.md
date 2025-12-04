# 배포 가이드

이 가이드는 다양한 환경과 구성에서 Tunnel Relay를 배포하는 방법을 다룹니다.

## Oracle Cloud (주요)

### E2 Micro 인스턴스 설정

1. **인스턴스 생성**:
   - Shape: VM.Standard.E2.1.Micro (무료 티어)
   - OS: Ubuntu 22.04 또는 Oracle Linux
   - 공인 IPv4 주소 활성화

2. **보안 구성**:

   **VCN 보안 목록**:
   - TCP 포트 8080 및 25565에 대한 인그레스 규칙 추가
   - 소스: `0.0.0.0/0` (또는 알려진 IP로 제한)

   **OS 방화벽**:
   ```bash
   # Ubuntu/Debian
   sudo ufw allow 8080/tcp
   sudo ufw allow 25565/tcp
   sudo ufw allow 6060/tcp  # API 액세스용

   # Oracle Linux
   sudo firewall-cmd --permanent --add-port=8080/tcp
   sudo firewall-cmd --permanent --add-port=25565/tcp
   sudo firewall-cmd --permanent --add-port=6060/tcp
   sudo firewall-cmd --reload
   ```

3. **Go 설치**:
   ```bash
   # Go 다운로드 및 설치
   wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
   sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
   echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
   source ~/.bashrc
   ```

4. **애플리케이션 배포**:
   ```bash
   # 클론 및 빌드
   git clone <repository-url>
   cd tunnel
   make

   # 서버 시작
   ./bin/tunnel-server start
   ```

### Systemd 서비스 (권장)

자동 시작을 위한 systemd 서비스 생성:

```bash
sudo tee /etc/systemd/system/tunnel-relay.service > /dev/null <<EOF
[Unit]
Description=Tunnel Relay Server
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/tunnel
ExecStart=/home/ubuntu/tunnel/bin/tunnel-server start
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 활성화 및 시작
sudo systemctl daemon-reload
sudo systemctl enable tunnel-relay
sudo systemctl start tunnel-relay

# 상태 확인
sudo systemctl status tunnel-relay
```

## 대체 클라우드 제공자

### AWS EC2

1. **인스턴스 설정**:
   - t2.micro 또는 t3.micro (무료 티어 적용)
   - Ubuntu 22.04 LTS
   - 보안 그룹: 0.0.0.0/0에서 TCP 8080, 25565, 6060 허용

2. **방화벽**:
   ```bash
   sudo ufw allow 8080/tcp
   sudo ufw allow 25565/tcp
   sudo ufw allow 6060/tcp
   ```

### DigitalOcean Droplet

1. **Droplet 설정**:
   - 기본 플랜 ($6/월)
   - Ubuntu 22.04
   - 필요시 IPv6 활성화

2. **방화벽**:
   ```bash
   sudo ufw allow 8080/tcp
   sudo ufw allow 25565/tcp
   sudo ufw allow 6060/tcp
   ```

### Google Cloud Compute Engine

1. **VM 인스턴스**:
   - e2-micro (무료 티어)
   - Ubuntu 22.04
   - TCP 8080, 25565, 6060 방화벽 규칙

## 로컬 개발

### Docker 배포

```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tunnel-server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/tunnel-server .
EXPOSE 8080 25565 6060
CMD ["./tunnel-server", "start"]
```

```bash
# 빌드 및 실행
docker build -t tunnel-relay .
docker run -p 8080:8080 -p 25565:25565 -p 6060:6060 tunnel-relay
```

### Docker Compose

```yaml
# docker-compose.yml
version: '3.8'
services:
  tunnel-relay:
    build: .
    ports:
      - "8080:8080"   # 제어 포트
      - "25565:25565" # 게임 포트
      - "6060:6060"   # API 포트
    restart: unless-stopped
    volumes:
      - ./logs:/app/logs
```

## 고급 구성

### 다중 릴레이 (로드 밸런싱)

고가용성을 위해 여러 릴레이 서버 배포:

1. **DNS 라운드 로빈**: 릴레이 IP 간에 DNS 로테이션 구성
2. **클라이언트 구성**: 여러 릴레이 주소를 시도하도록 클라이언트 업데이트
3. **헬스 체크**: 로드 밸런서용 헬스 체크 엔드포인트 구현

### 리버스 프록시 설정

nginx 또는 caddy를 리버스 프록시로 사용:

```nginx
# nginx.conf
server {
    listen 80;
    server_name your-domain.com;

    # API 엔드포인트
    location /api/ {
        proxy_pass http://localhost:6060/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # 게임 트래픽
    location / {
        proxy_pass http://localhost:25565;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### SSL/TLS 종료

```nginx
# SSL 구성
server {
    listen 443 ssl http2;
    server_name your-domain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # 터널 릴레이로 프록시
    location / {
        proxy_pass http://localhost:25565;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## 모니터링 및 유지보수

### 로그 관리

```bash
# 로그 로테이션 설정
sudo tee /etc/logrotate.d/tunnel-relay > /dev/null <<EOF
/home/*/.tunnel-relay.log {
    daily
    rotate 7
    compress
    missingok
    notifempty
    create 0644 ubuntu ubuntu
    postrotate
        systemctl reload tunnel-relay
    endscript
}
EOF
```

### 헬스 체크

헬스 체크 스크립트 생성:

```bash
#!/bin/bash
# /usr/local/bin/tunnel-health-check

# 데몬 실행 중인지 확인
if ! pgrep -f "tunnel-server" > /dev/null; then
    echo "데몬이 실행 중이지 않습니다"
    exit 1
fi

# API 엔드포인트 확인
if ! curl -f http://localhost:6060/status > /dev/null 2>&1; then
    echo "API가 응답하지 않습니다"
    exit 1
fi

# 포트가 리스닝 중인지 확인
if ! netstat -tln | grep -q :8080; then
    echo "제어 포트가 리스닝하지 않습니다"
    exit 1
fi

if ! netstat -tln | grep -q :25565; then
    echo "게임 포트가 리스닝하지 않습니다"
    exit 1
fi

echo "모든 체크 통과"
exit 0
```

### 백업 전략

```bash
#!/bin/bash
# 일일 백업 스크립트
BACKUP_DIR="/var/backups/tunnel-relay"
DATE=$(date +%Y%m%d)

mkdir -p $BACKUP_DIR

# 구성 및 로그 백업
tar -czf $BACKUP_DIR/tunnel-relay-$DATE.tar.gz \
    /home/ubuntu/tunnel/ \
    /home/ubuntu/.tunnel-relay.log

# 최근 7일만 유지
find $BACKUP_DIR -name "tunnel-relay-*.tar.gz" -mtime +7 -delete
```

## 보안 강화

### 네트워크 보안

1. **API 액세스 제한**:
   ```bash
   # 로컬 API 액세스만 허용
   sudo iptables -A INPUT -p tcp --dport 6060 -s 127.0.0.1 -j ACCEPT
   sudo iptables -A INPUT -p tcp --dport 6060 -j DROP
   ```

2. **내부 IP 사용**: 릴레이가 내부 IP에만 바인딩하도록 구성

3. **VPN 액세스**: 릴레이 관리에 VPN 필요

### 애플리케이션 보안

1. **API 인증**: API 키 인증 구현
2. **속도 제한**: API 엔드포인트에 속도 제한 추가
3. **입력 검증**: 모든 입력 매개변수 검증
4. **TLS 암호화**: 모든 연결에 TLS 구현

### 시스템 보안

1. **정기 업데이트**:
   ```bash
   sudo apt update && sudo apt upgrade -y
   ```

2. **Fail2Ban**: SSH 보호를 위해 fail2ban 설치

3. **SSH 강화**:
   - 비밀번호 인증 비활성화
   - SSH 키만 사용
   - 기본 SSH 포트 변경
   - fail2ban 설치

## 배포 문제 해결

### 일반적인 문제

#### 포트가 이미 사용 중

```bash
# 포트를 사용 중인 프로세스 찾기
sudo netstat -tlnp | grep :8080

# 프로세스 종료
sudo kill -9 <PID>
```

#### 방화벽이 연결 차단

```bash
# 방화벽 상태 확인
sudo ufw status

# 테스트를 위해 일시적으로 비활성화
sudo ufw disable
```

#### 권한 문제

```bash
# 로그 파일 권한 수정
sudo chown ubuntu:ubuntu ~/.tunnel-relay.log
sudo chmod 644 ~/.tunnel-relay.log
```

#### 메모리 문제

메모리 사용량 모니터링:
```bash
# 메모리 사용량 확인
ps aux --sort=-%mem | head

# 필요시 스왑 추가
sudo fallocate -l 1G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

## 성능 튜닝

### 시스템 제한

```bash
# 파일 디스크립터 증가
echo "ubuntu soft nofile 65536" | sudo tee -a /etc/security/limits.conf
echo "ubuntu hard nofile 65536" | sudo tee -a /etc/security/limits.conf

# 네트워크 튜닝
sudo sysctl -w net.core.somaxconn=1024
sudo sysctl -w net.ipv4.tcp_max_syn_backlog=1024
```

### 애플리케이션 튜닝

- 고지연 연결에 대한 Yamux 구성 조정
- 다중 호스트용 연결 풀링 구현
- 로그 스트리밍 압축 추가

## 비용 최적화

### Oracle Cloud 무료 티어

- Always Free 리소스만 사용
- 한도 내 사용량 모니터링
- 사용량 임계값에 대한 알림 설정

### 예약 인스턴스

유료 배포의 경우:
- 예측 가능한 워크로드에 예약 인스턴스 사용
- 예상 부하에 따른 적절한 인스턴스 유형 선택
- 실제 사용 패턴에 따라 모니터링 및 확장