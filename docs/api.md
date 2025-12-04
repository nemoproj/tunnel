# API 문서

이 문서는 Tunnel Relay REST API 엔드포인트에 대한 상세 정보를 제공합니다.

## 기본 URL

모든 API 엔드포인트는 설정된 API 포트에서 제공됩니다 (기본값: `6060`).

## 엔드포인트

### GET /status

터널 릴레이 서버의 현재 상태를 반환합니다.

#### 응답

```json
{
  "public_ip": "203.0.113.1",
  "control_port": 8080,
  "game_port": 25565,
  "active_players": 2,
  "bytes_transferred": 15432,
  "tunnel_connected": true,
  "uptime_seconds": 3600
}
```

#### 응답 필드

| 필드 | 타입 | 설명 |
|------|------|------|
| `public_ip` | string | 릴레이 서버의 공인 IP 주소 |
| `control_port` | int | 호스트 연결에 사용되는 포트 |
| `game_port` | int | 플레이어 연결에 사용되는 포트 |
| `active_players` | int | 현재 연결된 플레이어 수 |
| `bytes_transferred` | int64 | 서버 시작 이후 전송된 총 바이트 |
| `tunnel_connected` | bool | 호스트 클라이언트 연결 여부 |
| `uptime_seconds` | int64 | 서버 가동 시간 (초) |

#### 요청 예시

```bash
curl http://localhost:6060/status
```

### GET /logs

Server-Sent Events (SSE)를 사용하여 실시간 서버 로그 스트림을 제공합니다.

#### 응답 형식

엔드포인트는 SSE 형식의 연속적인 로그 이벤트 스트림을 반환합니다:

```
data: [Control] Connection from 192.168.1.100:54321

data: [Game] Player connected: 203.0.113.50:25565

data: [Game] Player disconnected: 203.0.113.50:25565
```

#### 로그 유형

- `[Control]`: 호스트 클라이언트 연결 관련 메시지
- `[Game]`: 플레이어 연결 관련 메시지
- `[API]`: API 작업 관련 메시지

#### 요청 예시

```bash
# curl에서 -N 옵션으로 버퍼링 비활성화
curl -N http://localhost:6060/logs

# 또는 파일로 저장
curl -N http://localhost:6060/logs > server_logs.txt
```

#### JavaScript 예시

```javascript
const eventSource = new EventSource('http://localhost:6060/logs');

eventSource.onmessage = function(event) {
    console.log('새 로그:', event.data);
};

eventSource.onerror = function(event) {
    console.error('SSE 오류:', event);
};
```

## 오류 응답

모든 엔드포인트는 적절한 HTTP 상태 코드를 반환합니다:

- `200 OK`: 요청 성공
- `404 Not Found`: 엔드포인트를 찾을 수 없음
- `500 Internal Server Error`: 서버 오류

오류 응답에는 오류 세부 정보가 포함된 JSON 본문이 있습니다:

```json
{
  "error": "Internal server error",
  "message": "상세한 오류 메시지"
}
```

## 속도 제한

현재 API 엔드포인트에 속도 제한이 구현되어 있지 않습니다.

## 인증

현재 API 액세스에 인증이 필요하지 않습니다. 프로덕션 배포에서는 인증 구현을 고려하세요.

## 콘텐츠 타입

- 요청: 해당 없음 (GET만 지원)
- 응답: `/status`는 `application/json`, `/logs`는 `text/event-stream`