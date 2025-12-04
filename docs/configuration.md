# 구성 참조

이 문서는 Tunnel Relay에서 사용 가능한 모든 구성 옵션을 설명합니다.

## 서버 구성

### 명령줄 플래그

서버는 다음 명령줄 플래그를 허용합니다:

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| `--control-port` | 8080 | 호스트 클라이언트 연결 수락 포트 |
| `--game-port` | 25565 | 플레이어 연결 수락 포트 |
| `--api-port` | 6060 | REST API 포트 |
| `--monitor` | false | 서버 대신 TUI 모니터 실행 |
| `--daemon` | false | 데몬 모드용 내부 플래그 |

### 환경 변수

현재 환경 변수는 지원되지 않습니다. 모든 구성은 명령줄 플래그를 통해 수행됩니다.

### 구성 파일

현재 구성 파일은 지원되지 않습니다. 모든 설정은 명령줄 인수를 통해 전달됩니다.

## 클라이언트 구성

클라이언트는 구성을 위한 대화형 TUI를 사용합니다. 다음 설정을 구성할 수 있습니다:

| 설정 | 기본값 | 설명 |
|------|--------|------|
| 릴레이 서버 주소 | `134.185.100.194:8080` | 릴레이 서버 주소 |
| 로컬 서버 주소 | `localhost:25565` | 로컬 마인크래프트 서버 주소 |
| 공용 게임 포트 | `25565` | 사용자에게 표시되는 포트 |

## 파일 위치

### 서버 파일

| 파일 | 위치 | 설명 |
|------|------|------|
| PID 파일 | `~/.tunnel-relay.pid` | 실행 중인 데몬의 프로세스 ID |
| 로그 파일 | `~/.tunnel-relay.log` | 서버 로그 출력 |
| 바이너리 | `./bin/tunnel-server` | 서버 실행 파일 |

### 클라이언트 파일

| 파일 | 위치 | 설명 |
|------|------|------|
| 바이너리 | `./bin/tunnel-client` | 클라이언트 실행 파일 |

## 네트워크 구성

### 포트

#### 기본 포트

- **제어 포트 (8080)**: 호스트 클라이언트 연결용
  - 프로토콜: TCP
  - 방향: 인바운드 (릴레이 서버로)
  - 용도: Yamux 세션 설정

- **게임 포트 (25565)**: 플레이어 연결용
  - 프로토콜: TCP
  - 방향: 인바운드 (릴레이 서버로)
  - 용도: 마인크래프트 게임 트래픽

- **API 포트 (6060)**: 모니터링 및 로그용
  - 프로토콜: TCP
  - 방향: 로컬 액세스 (릴레이 서버로)
  - 용도: REST API 및 SSE 로그

#### 포트 커스터마이징

```bash
# 사용자 정의 포트
./bin/tunnel-server start --control-port=9000 --game-port=30000 --api-port=8080
```

### 방화벽 구성

#### Oracle Linux / RHEL / CentOS

```bash
# 포트 영구 추가
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --permanent --add-port=25565/tcp
sudo firewall-cmd --permanent --add-port=6060/tcp
sudo firewall-cmd --reload

# 상태 확인
sudo firewall-cmd --list-all
```

#### Ubuntu / Debian

```bash
# UFW 활성화 (아직 안 했다면)
sudo ufw enable

# 규칙 추가
sudo ufw allow 8080/tcp
sudo ufw allow 25565/tcp
sudo ufw allow 6060/tcp

# 상태 확인
sudo ufw status
```

#### iptables (수동)

```bash
# 특정 포트 허용
sudo iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 25565 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 6060 -j ACCEPT

# 규칙 저장 (배포판에 따라 다름)
sudo iptables-save > /etc/iptables/rules.v4
```

### 네트워크 보안 그룹 (클라우드)

#### Oracle Cloud

1. VCN → 보안 목록으로 이동
2. 인그레스 규칙 추가:
   - 소스 유형: CIDR
   - 소스 CIDR: `0.0.0.0/0`
   - IP 프로토콜: TCP
   - 대상 포트 범위: `8080,25565,6060`

#### AWS EC2

1. EC2 → 보안 그룹으로 이동
2. 인바운드 규칙 추가:
   - 유형: 사용자 정의 TCP
   - 포트 범위: `8080, 25565, 6060`
   - 소스: `0.0.0.0/0`

#### Google Cloud

1. VPC 네트워크 → 방화벽으로 이동
2. 방화벽 규칙 생성:
   - 대상: 네트워크의 모든 인스턴스
   - 소스 IP 범위: `0.0.0.0/0`
   - 프로토콜 및 포트: `tcp:8080, tcp:25565, tcp:6060`

## Yamux 구성

서버는 다음 기본 설정으로 Yamux를 멀티플렉싱에 사용합니다:

```go
config := yamux.DefaultConfig()
config.KeepAliveInterval = 10 * time.Second
config.EnableKeepAlive = true
config.LogOutput = logWriter
```

### Yamux 매개변수

| 매개변수 | 기본값 | 설명 |
|----------|--------|------|
| `AcceptBacklog` | 256 | 수락 백로그 |
| `EnableKeepAlive` | true | keep-alive 메시지 전송 |
| `KeepAliveInterval` | 10s | keep-alive 간격 |
| `MaxStreamWindowSize` | 256KB | 최대 스트림 윈도우 크기 |
| `StreamOpenTimeout` | 60s | 스트림 열기 타임아웃 |
| `StreamCloseTimeout` | 5s | 스트림 닫기 타임아웃 |

### Yamux 튜닝

고지연 연결의 경우 타임아웃 조정이 필요할 수 있습니다:

```go
config.StreamOpenTimeout = 120 * time.Second
config.StreamCloseTimeout = 10 * time.Second
config.KeepAliveInterval = 30 * time.Second
```

## 로깅 구성

### 로그 레벨

현재 애플리케이션은 구성 가능한 레벨 없이 간단한 로깅을 사용합니다. 모든 로그는 다음에 기록됩니다:

- **파일**: `~/.tunnel-relay.log`
- **콘솔**: 포그라운드 모드에서 실행 시
- **API**: `/logs` 엔드포인트를 통해 사용 가능

### 로그 로테이션

디스크 공간 문제를 방지하기 위해 로그 로테이션 설정:

```bash
# /etc/logrotate.d/tunnel-relay
/home/*/.tunnel-relay.log {
    daily
    rotate 7
    compress
    missingok
    notifempty
    create 0644 ubuntu ubuntu
}
```

### 로그 형식

로그는 다음 형식을 따릅니다:
```
[컴포넌트] 세부 정보가 있는 메시지
```

예시:
```
[Control] Connection from 192.168.1.100:54321
[Game] Player connected: 203.0.113.50:25565
[API] Server started on :6060
```

## 시스템 제한

### 파일 디스크립터

높은 연결 수를 위해 시스템 제한 증가:

```bash
# /etc/security/limits.conf
* soft nofile 65536
* hard nofile 65536
```

### 네트워크 튜닝

```bash
# /etc/sysctl.conf
net.core.somaxconn = 1024
net.ipv4.tcp_max_syn_backlog = 1024
net.ipv4.ip_local_port_range = 1024 65535
```

변경 적용:
```bash
sudo sysctl -p
```

## 성능 튜닝

### 연결 제한

| 설정 | 기본값 | 권장값 | 설명 |
|------|--------|--------|------|
| 최대 연결 수 | 무제한 | 1000 | 총 동시 연결 수 |
| 스트림 타임아웃 | 60s | 120s | 스트림 작업 타임아웃 |
| Keep-alive | 10s | 30s | 연결 keep-alive 간격 |

### 메모리 튜닝

애플리케이션은 경량으로 설계되었습니다:

- **기본 메모리**: ~10-20MB
- **연결당**: ~8KB
- **스트림당**: ~4KB

### CPU 튜닝

- **기본 CPU**: 최소 (현대 하드웨어에서 < 1%)
- **네트워크 I/O**: CPU 사용량은 연결 수와 트래픽에 따라 확장

## 보안 구성

### 네트워크 보안

1. **API 액세스 제한**:
   ```bash
   # API에 로컬 액세스만 허용
   sudo iptables -A INPUT -p tcp --dport 6060 ! -s 127.0.0.1 -j DROP
   ```

2. **내부 IP 사용**: 서비스를 내부 인터페이스에만 바인딩

3. **VPN 요구사항**: 관리 액세스에 VPN 필요

### 애플리케이션 보안

1. **입력 검증**: 모든 입력이 검증됨
2. **인증 없음**: 현재 인증 불필요 (프로덕션에서는 추가)
3. **속도 제한**: 구현되지 않음 (추가 고려)

### TLS 구성

현재 TLS는 구현되지 않았습니다. 프로덕션용:

1. **TLS 종료**: 리버스 프록시 사용 (nginx/caddy)
2. **클라이언트 인증서**: 상호 TLS 구현
3. **API TLS**: API 엔드포인트에 TLS 활성화

## 모니터링 구성

### 메트릭

API를 통해 사용 가능한 메트릭:

- 연결 수
- 전송된 바이트
- 가동 시간
- 활성 플레이어
- 터널 상태

### 헬스 체크

헬스 체크 엔드포인트 스크립트 생성:

```bash
#!/bin/bash
# 데몬 프로세스 확인
pgrep -f "tunnel-server" > /dev/null || exit 1

# API 응답성 확인
curl -f http://localhost:6060/status > /dev/null || exit 1

# 포트 리스닝 확인
netstat -tln | grep -q :8080 || exit 1
netstat -tln | grep -q :25565 || exit 1

exit 0
```

### 알림

다음에 대한 알림 설정:

- 프로세스 다운
- 높은 메모리 사용량
- 포트 리스닝 안 함
- API 응답 없음
- 높은 오류율

## 백업 구성

### 백업할 파일

- 구성 파일 (구현 시)
- 로그 파일
- 바이너리 백업

### 백업 스크립트

```bash
#!/bin/bash
BACKUP_DIR="/var/backups/tunnel-relay"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR

# 애플리케이션 및 로그 백업
tar -czf $BACKUP_DIR/backup-$DATE.tar.gz \
    -C /home/ubuntu tunnel/ \
    .tunnel-relay.log

# 이전 백업 정리 (7일 유지)
find $BACKUP_DIR -name "backup-*.tar.gz" -mtime +7 -delete
```

## 구성 문제 해결

### 디버그 구성

디버그 로깅 활성화:

```bash
# 상세 출력으로 포그라운드에서 실행
./bin/tunnel-server --control-port=8080 --game-port=25565 --api-port=6060
```

### 진단 명령어

```bash
# 프로세스 상태 확인
ps aux | grep tunnel-server

# 네트워크 연결 확인
netstat -tlnp | grep :8080
netstat -tlnp | grep :25565

# 로그 확인
tail -f ~/.tunnel-relay.log

# API 확인
curl http://localhost:6060/status

# 방화벽 확인
sudo ufw status
sudo iptables -L
```

### 일반적인 구성 문제

1. **포트 충돌**: 포트가 이미 사용 중인지 확인
2. **방화벽 규칙**: 방화벽이 트래픽을 허용하는지 확인
3. **권한**: 적절한 파일 권한 확인
4. **리소스 제한**: ulimit 및 시스템 제한 확인