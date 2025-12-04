# 개발 가이드

이 가이드는 Tunnel Relay 프로젝트에 기여하거나 수정하려는 개발자를 위한 정보를 제공합니다.

## 사전 요구사항

- Go 1.21 이상
- Git
- 네트워킹 개념에 대한 기본 이해
- 터미널 기반 개발에 대한 익숙함

## 시작하기

### 클론 및 설정

```bash
git clone <repository-url>
cd tunnel

# 의존성 설치
go mod download

# 프로젝트 빌드
make

# 테스트 실행
go test ./...
```

### 개발 워크플로우

1. `main`에서 기능 브랜치 생성
2. 변경사항 구현
3. 테스트 실행 및 통과 확인
4. 필요시 문서 업데이트
5. 풀 리퀘스트 제출

## 프로젝트 구조

```
tunnel/
├── cmd/
│   ├── client/          # 호스트 클라이언트 애플리케이션
│   │   └── main.go      # 클라이언트 TUI 및 연결 로직
│   └── server/          # 릴레이 서버 애플리케이션
│       ├── main.go      # 서버 데몬 및 CLI 로직
│       └── tui.go       # 서버 모니터링 TUI
├── pkg/
│   ├── daemon/          # 데몬 관리 유틸리티
│   │   └── daemon.go    # PID 파일, 프로세스 관리
│   ├── relay/           # 코어 릴레이 기능
│   │   ├── relay.go     # 메인 릴레이 로직 및 멀티플렉싱
│   │   └── api.go       # REST API 엔드포인트
│   └── tunnel/          # 향후 사용을 위해 예약됨
├── docs/                # 문서
├── bin/                 # 빌드된 바이너리 (생성됨)
├── go.mod               # Go 모듈 정의
├── go.sum               # 의존성 체크섬
├── Makefile             # 빌드 자동화
└── README.md            # 메인 문서
```

## 주요 컴포넌트

### 서버 컴포넌트

#### 릴레이 코어 (`pkg/relay/relay.go`)

코어 릴레이 기능이 처리하는 것들:

- **제어 서버**: 포트 8080에서 호스트 클라이언트 연결 수락
- **게임 서버**: 포트 25565에서 플레이어 연결 수락
- **Yamux 멀티플렉싱**: 단일 TCP 연결을 통한 가상 스트림 관리
- **트래픽 카운팅**: 전송된 바이트 추적
- **로깅**: 모든 리스너에게 이벤트 브로드캐스트

#### API 서버 (`pkg/relay/api.go`)

모니터링을 위한 REST 엔드포인트 제공:

- `/status`: JSON 상태 정보
- `/logs`: Server-Sent Events 로그 스트림

#### 데몬 관리 (`pkg/daemon/daemon.go`)

백그라운드 프로세스 관리 처리:

- PID 파일 관리
- 프로세스 라이프사이클 (시작/중지/상태)
- 정상 종료를 위한 시그널 처리

### 클라이언트 컴포넌트

#### 호스트 클라이언트 (`cmd/client/main.go`)

다음을 수행하는 TUI 애플리케이션:

- 구성 프롬프트 (릴레이 주소, 로컬 서버, 포트)
- 릴레이와 Yamux 연결 설정
- 릴레이 스트림과 로컬 마인크래프트 서버 간 트래픽 프록시
- 실시간 연결 상태 및 로그 표시

## 아키텍처 세부사항

### 연결 흐름

1. **호스트 연결**: 클라이언트가 릴레이의 제어 포트 (8080)에 연결
2. **Yamux 세션**: 연결을 통해 Yamux 클라이언트 세션 설정
3. **스트림 처리**: 호스트가 릴레이에서 스트림 수락, 각 스트림은 플레이어를 나타냄
4. **플레이어 프록시**: 각 스트림이 로컬 마인크래프트 서버 (25565)에 연결
5. **양방향 트래픽**: 플레이어 ↔ 릴레이 ↔ 호스트 ↔ 마인크래프트 간 데이터 흐름

### Yamux 구성

```go
config := yamux.DefaultConfig()
config.KeepAliveInterval = 10 * time.Second
config.LogOutput = logWriter
```

주요 설정:
- 연결 유지를 위해 10초마다 keep-alive
- 디버깅을 위한 사용자 정의 로그 출력
- 기본 멀티플렉싱 매개변수

### 시그널 처리

데몬이 응답하는 시그널:
- `SIGTERM`: 정상 종료 (PID 파일 제거)
- `SIGINT`: 즉시 종료

## 테스트

### 유닛 테스트

```bash
# 모든 테스트 실행
go test ./...

# 커버리지와 함께 실행
go test -cover ./...

# 특정 패키지 실행
go test ./pkg/relay
```

### 통합 테스트

통합 테스트의 경우:

1. 릴레이 서버 인스턴스 시작
2. 호스트 클라이언트 실행
3. 테스트 클라이언트로 연결 (예: netcat 또는 마인크래프트 클라이언트)
4. 데이터가 올바르게 흐르는지 확인

### 수동 테스트

```bash
# 서버 시작/종료 테스트
./bin/tunnel-server start
./bin/tunnel-server status
./bin/tunnel-server stop

# API 엔드포인트 테스트
curl http://localhost:6060/status
curl -N http://localhost:6060/logs

# 클라이언트 연결 테스트 (실행 중인 서버 필요)
./bin/tunnel-client
```

## 디버깅

### 로그 파일

- 서버 로그: `~/.tunnel-relay.log`
- 실시간 보기: `tunnel-server monitor`
- 프로그래매틱 액세스: API 로그 엔드포인트

### 일반 디버그 명령어

```bash
# 포트 사용 중인지 확인
netstat -tlnp | grep :8080
netstat -tlnp | grep :25565

# 네트워크 연결 모니터링
ss -tlnp

# 데몬 프로세스 확인
ps aux | grep tunnel-server

# 최근 로그 보기
tail -f ~/.tunnel-relay.log
```

### 디버그 모드

자세한 디버깅을 위해 포그라운드에서 서버 실행:

```bash
./bin/tunnel-server --control-port=8080 --game-port=25565 --api-port=6060
```

## 성능 고려사항

### 리소스 사용량

- **메모리**: 최소 (~서버 10-20MB, ~클라이언트 5-10MB)
- **CPU**: 낮은 오버헤드, 주로 네트워크 I/O 바운드
- **네트워크**: 호스트당 단일 TCP 연결, 멀티플렉싱된 스트림

### 확장성

현재 제한사항:
- 릴레이 서버당 단일 호스트 연결
- 여러 릴레이 간 로드 밸런싱 없음
- 연결 풀링 없음

## 보안 고려사항

### 현재 보안

- 인증 불필요
- 암호화 없음 (기본 TCP에 의존)
- 공개 API 엔드포인트

### 권장 개선사항

- TLS 암호화 구현
- API 인증 추가
- 속도 제한
- 입력 검증
- 방화벽 구성 검증

## 기여 가이드라인

### 코드 스타일

- 표준 Go 규칙 따르기
- 포맷팅에 `gofmt` 사용
- 내보낸 함수에 주석 추가
- 새 기능에 유닛 테스트 포함

### 커밋 메시지

conventional commit 형식 사용:

```
feat: 새 기능 추가
fix: 버그 해결
docs: 문서 업데이트
refactor: 코드 구조 개선
```

### 풀 리퀘스트 프로세스

1. 모든 테스트 통과 확인
2. API 변경에 대한 문서 업데이트
3. 호환성 깨지는 변경에 대한 마이그레이션 노트 추가
4. 메인테이너 리뷰 받기

## 배포

### 프로덕션 고려사항

- 프로세스 관리에 systemd 또는 유사 도구 사용
- 로그 로테이션 구성
- 모니터링 및 알림 설정
- API에 리버스 프록시 사용 (nginx, caddy)
- 백업 전략 구현

### Docker 지원

향후 개선사항으로 포함될 수 있음:

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o tunnel-server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/tunnel-server .
CMD ["./tunnel-server", "start"]
```